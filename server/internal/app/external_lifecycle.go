package app

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var devicePattern = regexpDeviceID()

func regexpDeviceID() *regexp.Regexp {
	return regexp.MustCompile("^[a-f0-9]{32}$")
}

// Introspection is authoritative. ChimePort never trusts old access or refresh
// tokens after the owning TMS user/device was disabled, revoked, or had a
// password change. No positive result is cached: outages fail closed.
func (a *App) validateTrustedDevice(ctx context.Context, app trustedApp, recipient, deviceID string) (bool, error) {
	if a.externalValidate != nil {
		return a.externalValidate(ctx, app, recipient, deviceID)
	}
	body, _ := json.Marshal(map[string]string{"recipient": recipient, "device_id": deviceID})
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, app.origin+"/api/notifications/chimeport/introspect", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+app.token)
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, TLSHandshakeTimeout: 2 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, errors.New("unavailable introspection")
	}
	var data struct {
		Active bool `json:"active"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4096))
	if err = decoder.Decode(&data); err != nil {
		return false, err
	}
	return data.Active, nil
}
func (a *App) verifyExternalIdentity(w http.ResponseWriter, r *http.Request, origin, secret, recipient, deviceID string) bool {
	valid, err := a.validateTrustedDevice(r.Context(), trustedApp{origin: origin, token: secret}, recipient, deviceID)
	if err != nil {
		failure(w, 503, "identity_unavailable", "Trusted application identity verification unavailable")
		return false
	}
	if !valid {
		failure(w, 401, "device_revoked", "Device is no longer authorized by its application")
		return false
	}
	return true
}
func (a *App) verifyExternalDevice(w http.ResponseWriter, r *http.Request, s trustedSession) bool {
	valid, err := a.validateTrustedDevice(r.Context(), s.app, s.recipient, s.deviceID)
	if err != nil {
		failure(w, 503, "identity_unavailable", "Trusted application identity verification unavailable")
		return false
	}
	if !valid {
		if err = a.revokeExternalSession(r.Context(), s.id); err != nil {
			a.err(w, err)
			return false
		}
		failure(w, 401, "device_revoked", "Device is no longer authorized by its application")
		return false
	}
	return true
}
func (a *App) revokeExternalSession(ctx context.Context, id int64) error {
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE external_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE id=?", now(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM external_webpush WHERE session_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *App) externalRefresh(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	token, retryID := in["refresh_token"], in["retry_id"]
	if !strings.HasPrefix(token, "cp_r_") || len(token) > 256 || !devicePattern.MatchString(retryID) {
		failure(w, 401, "invalid_refresh", "Invalid refresh credentials")
		return
	}
	hash, retryHash := digest(token), digest(retryID)
	var session trustedSession
	var encrypted string
	err := a.Store.DB.QueryRowContext(r.Context(),
		"SELECT s.id,a.id,a.slug,a.origin,a.token_encrypted,s.recipient,s.device_id FROM external_sessions s JOIN external_apps a ON a.id=s.app_id WHERE s.refresh_hash=? AND s.refresh_expires_at>? AND s.revoked_at IS NULL AND a.disabled=0",
		hash, now()).Scan(&session.id, &session.app.id, &session.app.slug, &session.app.origin, &encrypted, &session.recipient, &session.deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		// A lost HTTP response must not log the device out: retries using the
		// SAME random per-rotation retry secret receive the identical response.
		// Replays with a DIFFERENT secret revoke the session (token theft).
		var sessionID int64
		var expectedRetry, replayCipher string
		checkErr := a.Store.DB.QueryRowContext(r.Context(),
			"SELECT session_id,retry_hash,reply_encrypted FROM external_used_refresh WHERE refresh_hash=?",
			hash).Scan(&sessionID, &expectedRetry, &replayCipher)
		if errors.Is(checkErr, sql.ErrNoRows) {
			failure(w, 401, "invalid_refresh", "Invalid or expired refresh token")
			return
		}
		if checkErr != nil {
			a.err(w, checkErr)
			return
		}
		if subtle.ConstantTimeCompare([]byte(expectedRetry), []byte(retryHash)) != 1 {
			if err = a.revokeExternalSession(r.Context(), sessionID); err != nil {
				a.err(w, err)
				return
			}
			failure(w, 401, "refresh_replay", "Refresh token reuse detected; device revoked")
			return
		}
		var accessExpiry, currentRefreshHash string
		err = a.Store.DB.QueryRowContext(r.Context(),
			"SELECT s.id,a.id,a.slug,a.origin,a.token_encrypted,s.recipient,s.device_id,s.expires_at,s.refresh_hash FROM external_sessions s JOIN external_apps a ON a.id=s.app_id WHERE s.id=? AND s.refresh_expires_at>? AND s.revoked_at IS NULL AND a.disabled=0",
			sessionID, now()).Scan(&session.id, &session.app.id, &session.app.slug, &session.app.origin, &encrypted, &session.recipient, &session.deviceID, &accessExpiry, &currentRefreshHash)
		if err != nil {
			failure(w, 401, "invalid_refresh", "Device session expired or revoked")
			return
		}
		session.app.token, err = a.Store.decryptToken(encrypted)
		if err != nil {
			a.err(w, err)
			return
		}
		if !a.verifyExternalDevice(w, r, session) {
			return
		}
		plaintext, err := a.Store.decryptToken(replayCipher)
		if err != nil {
			a.err(w, err)
			return
		}
		var reply map[string]any
		if err = json.Unmarshal([]byte(plaintext), &reply); err != nil {
			a.err(w, err)
			return
		}
		savedRefresh, ok := reply["refresh_token"].(string)
		if !ok || subtle.ConstantTimeCompare([]byte(digest(savedRefresh)), []byte(currentRefreshHash)) != 1 {
			failure(w, 401, "invalid_refresh", "A more recent token rotation superseded this retry")
			return
		}
		// A retry delayed beyond access expiry returns the current refresh
		// secret, but tells the client to rotate again immediately.
		accessDate, parseErr := time.Parse(time.RFC3339Nano, accessExpiry)
		if parseErr != nil {
			a.err(w, parseErr)
			return
		}
		remaining := int(time.Until(accessDate).Seconds())
		if remaining < 1 {
			remaining = 1
		}
		if remaining > 900 {
			remaining = 900
		}
		reply["expires_in"] = remaining
		respond(w, 200, reply)
		return
	}
	if err != nil {
		a.err(w, err)
		return
	}
	session.app.token, err = a.Store.decryptToken(encrypted)
	if err != nil {
		a.err(w, err)
		return
	}
	if !a.verifyExternalDevice(w, r, session) {
		return
	}

	access, accessHash, err := newSecret("cp_e_")
	if err != nil {
		a.err(w, err)
		return
	}
	refresh, newHash, err := newSecret("cp_r_")
	if err != nil {
		a.err(w, err)
		return
	}
	timestamp := time.Now().UTC()
	accessExpiry := timestamp.Add(15 * time.Minute).Format(time.RFC3339Nano)
	refreshExpiry := timestamp.Add(90 * 24 * time.Hour).Format(time.RFC3339Nano)
	reply := map[string]any{
		"access_token": access, "refresh_token": refresh, "token_type": "Bearer",
		"expires_in": 900, "refresh_expires_in": 7776000, "instance_id": a.Store.InstanceID,
	}
	payload, err := json.Marshal(reply)
	if err != nil {
		a.err(w, err)
		return
	}
	replayCipher, err := a.Store.encryptToken(string(payload))
	if err != nil {
		a.err(w, err)
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(),
		"UPDATE external_sessions SET token_hash=?,expires_at=?,refresh_hash=?,refresh_expires_at=?,last_seen=? WHERE id=? AND refresh_hash=? AND revoked_at IS NULL",
		accessHash, accessExpiry, newHash, refreshExpiry, now(), session.id, hash)
	if err != nil {
		a.err(w, err)
		return
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		failure(w, 503, "refresh_race", "Concurrent token rotation; retry with same retry_id")
		return
	}
	_, err = tx.ExecContext(r.Context(),
		"INSERT INTO external_used_refresh(refresh_hash,session_id,retry_hash,reply_encrypted,used_at) VALUES(?,?,?,?,?)",
		hash, session.id, retryHash, replayCipher, now())
	if err != nil {
		a.err(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, reply)
}

func (a *App) externalAccessStillValid(ctx context.Context, s trustedSession) bool {
	var active int
	err := a.Store.DB.QueryRowContext(ctx,
		"SELECT 1 FROM external_sessions WHERE id=? AND token_hash=? AND expires_at>? AND revoked_at IS NULL",
		s.id, s.tokenHash, now()).Scan(&active)
	return err == nil
}
func (a *App) externalDeviceStatus(ctx context.Context, s trustedSession) bool {
	valid, err := a.validateTrustedDevice(ctx, s.app, s.recipient, s.deviceID)
	if err != nil {
		slog.Warn("trusted application introspection unavailable")
		return false
	}
	if !valid {
		if err = a.revokeExternalSession(ctx, s.id); err != nil {
			slog.Error("failed to revoke denied external device", "error", err)
		}
		return false
	}
	return true
}

// TMS device registry remains the authority even when CP cleanup fails.
func (a *App) externalRevokeDevice(w http.ResponseWriter, r *http.Request) {
	app, ok := a.externalApp(w, r)
	if !ok {
		return
	}
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	if !recipientPattern.MatchString(in["recipient"]) || !devicePattern.MatchString(in["device_id"]) {
		failure(w, 400, "invalid_device", "Invalid device identifier")
		return
	}
	if _, err := a.Store.DB.ExecContext(r.Context(),
		"DELETE FROM external_sessions WHERE app_id=? AND recipient=? AND device_id=?",
		app.id, in["recipient"], in["device_id"]); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"revoked": true})
}
func (a *App) externalRevokeRecipient(w http.ResponseWriter, r *http.Request) {
	app, ok := a.externalApp(w, r)
	if !ok {
		return
	}
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	if !recipientPattern.MatchString(in["recipient"]) {
		failure(w, 400, "invalid_recipient", "Invalid recipient identifier")
		return
	}
	if _, err := a.Store.DB.ExecContext(r.Context(),
		"DELETE FROM external_sessions WHERE app_id=? AND recipient=?", app.id, in["recipient"]); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"revoked": true})
}
