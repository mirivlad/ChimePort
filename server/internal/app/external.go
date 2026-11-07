package app

// Trusted applications own isolated recipient streams; recipients are not local users.
import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var recipientPattern = regexp.MustCompile("^[A-Za-z0-9._-]{1,128}$")
var appSlugPattern = regexp.MustCompile("^[a-z][a-z0-9_-]{2,63}$")
var verifierPattern = regexp.MustCompile("^[A-Za-z0-9._~-]{43,128}$")

type trustedApp struct {
	id           int64
	slug, origin string
	token        string
}
type trustedSession struct {
	id        int64
	app       trustedApp
	recipient string
	deviceID  string
	tokenHash string
}

func validAppOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" &&
		u.Host == strings.ToLower(u.Host)
}
func (a *App) listExternalApps(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT id,slug,origin,disabled,created_at FROM external_apps ORDER BY id")
	if err != nil {
		a.err(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id int64
		var slug, origin, created string
		var disabled bool
		if err = rows.Scan(&id, &slug, &origin, &disabled, &created); err != nil {
			a.err(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "slug": slug, "origin": origin, "disabled": disabled, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *App) createExternalApp(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	slug, origin := in["slug"], in["origin"]
	if !appSlugPattern.MatchString(slug) || !validAppOrigin(origin) {
		failure(w, 400, "invalid_application", "Use unique slug and an HTTPS origin")
		return
	}
	token, hash, err := newSecret("cp_a_")
	if err != nil {
		a.err(w, err)
		return
	}
	encrypted, err := a.Store.encryptToken(token)
	if err != nil {
		a.err(w, err)
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(), "INSERT INTO external_apps(slug,origin,token_hash,token_encrypted,created_at) VALUES(?,?,?,?,?)", slug, origin, hash, encrypted, now())
	if err != nil {
		failure(w, 409, "application_exists", "Application already exists")
		return
	}
	id, _ := result.LastInsertId()
	respond(w, 201, map[string]any{"id": id, "slug": slug, "origin": origin, "token": token})
}
func (a *App) changeExternalApp(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "Application not found")
		return
	}
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Disabled == nil {
		failure(w, 400, "invalid_application", "disabled boolean is required")
		return
	}
	disabled := 0
	if *in.Disabled {
		disabled = 1
	}
	result, err := a.Store.DB.ExecContext(r.Context(), "UPDATE external_apps SET disabled=? WHERE id=?", disabled, id)
	if err != nil {
		a.err(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		failure(w, 404, "not_found", "Application not found")
		return
	}
	respond(w, 200, map[string]bool{"disabled": *in.Disabled})
}
func (a *App) externalApp(w http.ResponseWriter, r *http.Request) (trustedApp, bool) {
	token := bearerToken(r)
	if !strings.HasPrefix(token, "cp_a_") {
		failure(w, 401, "unauthorized", "Application token required")
		return trustedApp{}, false
	}
	var app trustedApp
	err := a.Store.DB.QueryRowContext(r.Context(), "SELECT id,slug,origin FROM external_apps WHERE token_hash=? AND disabled=0", digest(token)).Scan(&app.id, &app.slug, &app.origin)
	app.token = token
	if err != nil {
		failure(w, 401, "unauthorized", "Invalid or revoked application token")
		return trustedApp{}, false
	}
	return app, true
}
func (a *App) externalSession(w http.ResponseWriter, r *http.Request) (trustedSession, bool) {
	token := bearerToken(r)
	if !strings.HasPrefix(token, "cp_e_") {
		failure(w, 401, "unauthorized", "Device session required")
		return trustedSession{}, false
	}
	var session trustedSession
	var encrypted string
	err := a.Store.DB.QueryRowContext(r.Context(),
		"SELECT s.id,a.id,a.slug,a.origin,a.token_encrypted,s.recipient,s.device_id FROM external_sessions s JOIN external_apps a ON a.id=s.app_id WHERE s.token_hash=? AND s.expires_at>? AND s.refresh_expires_at>? AND s.revoked_at IS NULL AND a.disabled=0",
		digest(token), now(), now()).Scan(&session.id, &session.app.id, &session.app.slug, &session.app.origin, &encrypted, &session.recipient, &session.deviceID)
	if err != nil {
		failure(w, 401, "unauthorized", "Invalid or expired device session")
		return trustedSession{}, false
	}
	session.tokenHash = digest(token)
	session.app.token, err = a.Store.decryptToken(encrypted)
	if err != nil {
		a.err(w, err)
		return trustedSession{}, false
	}
	if !a.verifyExternalDevice(w, r, session) {
		return trustedSession{}, false
	}
	if _, err = a.Store.DB.ExecContext(r.Context(), "UPDATE external_sessions SET last_seen=? WHERE id=?", now(), session.id); err != nil {
		a.err(w, err)
		return trustedSession{}, false
	}
	return session, true
}
func (a *App) publishExternal(w http.ResponseWriter, r *http.Request) {
	app, ok := a.externalApp(w, r)
	if !ok {
		return
	}
	var in struct {
		Recipient string `json:"recipient"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Priority  string `json:"priority"`
		URL       string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !recipientPattern.MatchString(in.Recipient) || strings.TrimSpace(in.Title) == "" ||
		len([]rune(in.Title)) > 200 || len(in.Body) > 16384 || !priorityValues[in.Priority] {
		failure(w, 400, "invalid_message", "Invalid recipient/title/body/priority")
		return
	}
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || u.User != nil || u.Fragment != "" || u.Scheme+"://"+u.Host != app.origin {
			failure(w, 400, "invalid_url", "Link must use trusted application origin")
			return
		}
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) > 128 || strings.ContainsRune(key, 13) || strings.ContainsRune(key, 10) || strings.ContainsRune(key, 0) {
		failure(w, 400, "invalid_idempotency_key", "Invalid idempotency key")
		return
	}
	canonical, _ := json.Marshal(in)
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	if key != "" {
		var id int64
		var oldHash string
		err = tx.QueryRow("SELECT message_id,payload_hash FROM external_idempotency WHERE app_id=? AND idem_key=?", app.id, key).Scan(&id, &oldHash)
		if err == nil {
			if oldHash != hash {
				failure(w, 409, "idempotency_conflict", "Key already used with different payload")
				return
			}
			respond(w, 200, map[string]any{"id": strconv.FormatInt(id, 10), "stored": true, "duplicate": true})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			a.err(w, err)
			return
		}
	}
	minute := time.Now().UTC().Unix() / 60
	var oldMinute int64
	var accepted int
	err = tx.QueryRow("SELECT minute,accepted FROM external_publication_windows WHERE app_id=?", app.id).Scan(&oldMinute, &accepted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.err(w, err)
		return
	}
	if oldMinute == minute && accepted >= a.PublishRatePerMinute {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "rate_limited", "Application publish limit exceeded")
		return
	}
	if _, err = tx.Exec("INSERT INTO external_publication_windows(app_id,minute,accepted) VALUES(?,?,1) ON CONFLICT(app_id) DO UPDATE SET minute=excluded.minute,accepted=CASE WHEN external_publication_windows.minute=excluded.minute THEN external_publication_windows.accepted+1 ELSE 1 END", app.id, minute); err != nil {
		a.err(w, err)
		return
	}
	created := now()
	result, err := tx.Exec("INSERT INTO external_messages(app_id,recipient,title,body,priority,url,created_at) VALUES(?,?,?,?,?,?,?)", app.id, in.Recipient, in.Title, in.Body, in.Priority, in.URL, created)
	if err != nil {
		a.err(w, err)
		return
	}
	id, _ := result.LastInsertId()
	if key != "" {
		if _, err = tx.Exec("INSERT INTO external_idempotency(app_id,idem_key,payload_hash,message_id) VALUES(?,?,?,?)", app.id, key, hash, id); err != nil {
			a.err(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	a.signalExternal(app.id, in.Recipient, id)
	a.queueExternalPush(app.id, in.Recipient, id, in.Title, in.Body, app.slug, in.Priority, in.URL)
	respond(w, 201, map[string]any{"id": strconv.FormatInt(id, 10), "stored": true, "created_at": created})
}
func (a *App) createExternalTicket(w http.ResponseWriter, r *http.Request) {
	app, ok := a.externalApp(w, r)
	if !ok {
		return
	}
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	recipient, challenge, deviceID := in["recipient"], in["code_challenge"], in["device_id"]
	bytes, err := base64.RawURLEncoding.DecodeString(challenge)
	if !recipientPattern.MatchString(recipient) || !devicePattern.MatchString(deviceID) || err != nil || len(bytes) != 32 {
		failure(w, 400, "invalid_ticket", "Recipient and PKCE S256 challenge required")
		return
	}
	ticket, hash, err := newSecret("cp_k_")
	if err != nil {
		a.err(w, err)
		return
	}
	expires := time.Now().UTC().Add(90 * time.Second).Format(time.RFC3339Nano)
	if _, err = a.Store.DB.ExecContext(r.Context(), "INSERT INTO external_tickets(ticket_hash,app_id,recipient,device_id,challenge,expires_at) VALUES(?,?,?,?,?,?)", hash, app.id, recipient, deviceID, challenge, expires); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 201, map[string]any{"ticket": ticket, "expires_in": 90})
}
func (a *App) exchangeExternalTicket(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	ticket, verifier := in["ticket"], in["code_verifier"]
	if !strings.HasPrefix(ticket, "cp_k_") || !verifierPattern.MatchString(verifier) {
		failure(w, 401, "invalid_ticket", "Invalid or expired ticket")
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	var appID int64
	var recipient, deviceID, expected string
	var origin, encrypted string
	err = tx.QueryRow("SELECT t.app_id,t.recipient,t.device_id,t.challenge,a.origin,a.token_encrypted FROM external_tickets t JOIN external_apps a ON a.id=t.app_id WHERE t.ticket_hash=? AND t.expires_at>? AND a.disabled=0", digest(ticket), now()).Scan(&appID, &recipient, &deviceID, &expected, &origin, &encrypted)
	if err != nil {
		failure(w, 401, "invalid_ticket", "Invalid or expired ticket")
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(computed), []byte(expected)) != 1 {
		failure(w, 401, "invalid_ticket", "Invalid or expired ticket")
		return
	}
	// Validate TMS account/device *before* consuming the one-use ticket.
	appSecret, err := a.Store.decryptToken(encrypted)
	if err != nil {
		a.err(w, err)
		return
	}
	if !a.verifyExternalIdentity(w, r, origin, appSecret, recipient, deviceID) {
		return
	}
	result, err := tx.Exec("DELETE FROM external_tickets WHERE ticket_hash=?", digest(ticket))
	if err != nil {
		a.err(w, err)
		return
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		failure(w, 401, "invalid_ticket", "Ticket was redeemed")
		return
	}
	access, accessHash, err := newSecret("cp_e_")
	if err != nil {
		a.err(w, err)
		return
	}
	refresh, refreshHash, err := newSecret("cp_r_")
	if err != nil {
		a.err(w, err)
		return
	}
	timestamp := time.Now().UTC()
	accessExpiry := timestamp.Add(15 * time.Minute).Format(time.RFC3339Nano)
	refreshExpiry := timestamp.Add(90 * 24 * time.Hour).Format(time.RFC3339Nano)
	// Multiple logins for the same TMS device invalidate older sessions and
	// their web push subscriptions. The device registry is authoritative in TMS.
	if _, err = tx.Exec("DELETE FROM external_sessions WHERE app_id=? AND recipient=? AND device_id=?", appID, recipient, deviceID); err != nil {
		a.err(w, err)
		return
	}
	if _, err = tx.Exec("INSERT INTO external_sessions(app_id,recipient,device_id,token_hash,expires_at,refresh_hash,refresh_expires_at,created_at,last_seen) VALUES(?,?,?,?,?,?,?,?,?)", appID, recipient, deviceID, accessHash, accessExpiry, refreshHash, refreshExpiry, now(), now()); err != nil {
		a.err(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 900, "refresh_expires_in": 7776000, "instance_id": a.Store.InstanceID})
}
func (a *App) externalLogout(w http.ResponseWriter, r *http.Request) {
	s, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	if _, err := a.Store.DB.ExecContext(r.Context(), "DELETE FROM external_sessions WHERE id=?", s.id); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"logged_out": true})
}
func (a *App) externalInfo(w http.ResponseWriter, r *http.Request) {
	s, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	respond(w, 200, map[string]any{"instance_id": a.Store.InstanceID, "application": s.app.slug, "origin": s.app.origin, "recipient": s.recipient})
}
func (a *App) externalCursor(s trustedSession, last int64) string {
	raw := fmt.Sprintf("external:%s:%d:%s:%d", a.Store.InstanceID, s.app.id, s.recipient, last)
	sum := sha256.Sum256([]byte(a.cursorSecret + ":" + raw))
	return base64.RawURLEncoding.EncodeToString([]byte(raw)) + "." + base64.RawURLEncoding.EncodeToString(sum[:])
}
func (a *App) parseExternalCursor(s trustedSession, c string) (int64, error) {
	parts := strings.Split(c, ".")
	if len(parts) != 2 {
		return 0, errors.New("invalid cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256([]byte(a.cursorSecret + ":" + string(raw)))
	if subtle.ConstantTimeCompare(sig, sum[:]) != 1 {
		return 0, errors.New("bad signature")
	}
	prefix := fmt.Sprintf("external:%s:%d:%s:", a.Store.InstanceID, s.app.id, s.recipient)
	if !strings.HasPrefix(string(raw), prefix) {
		return 0, errors.New("wrong recipient")
	}
	last, err := strconv.ParseInt(strings.TrimPrefix(string(raw), prefix), 10, 64)
	if err != nil || last < 0 {
		return 0, errors.New("bad cursor")
	}
	return last, nil
}
func (a *App) historyExternal(w http.ResponseWriter, r *http.Request) {
	s, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			failure(w, 400, "invalid_limit", "Limit must be 1-200")
			return
		}
		limit = n
	}
	last := int64(0)
	if v := r.URL.Query().Get("after"); v != "" {
		var err error
		last, err = a.parseExternalCursor(s, v)
		if err != nil {
			failure(w, 400, "invalid_cursor", "Invalid cursor")
			return
		}
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT m.id,m.title,m.body,m.priority,m.url,m.created_at,rs.read_at FROM external_messages m LEFT JOIN external_read_states rs ON rs.message_id=m.id AND rs.app_id=? AND rs.recipient=? WHERE m.app_id=? AND m.recipient=? AND m.id>? ORDER BY m.id LIMIT ?", s.app.id, s.recipient, s.app.id, s.recipient, last, limit+1)
	if err != nil {
		a.err(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	more := false
	for rows.Next() {
		if len(items) == limit {
			more = true
			break
		}
		var id int64
		var title, body, priority, link, created string
		var readAt sql.NullString
		if err = rows.Scan(&id, &title, &body, &priority, &link, &created, &readAt); err != nil {
			a.err(w, err)
			return
		}
		items = append(items, map[string]any{"id": strconv.FormatInt(id, 10), "channel": s.app.slug, "title": title, "body": body, "priority": priority, "url": link, "created_at": created, "read_at": nullable(readAt)})
		last = id
	}
	if err = rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items, "next_cursor": a.externalCursor(s, last), "has_more": more})
}
func (a *App) markExternalRead(w http.ResponseWriter, r *http.Request)   { a.externalRead(w, r, true) }
func (a *App) markExternalUnread(w http.ResponseWriter, r *http.Request) { a.externalRead(w, r, false) }
func (a *App) externalRead(w http.ResponseWriter, r *http.Request, read bool) {
	s, ok := a.externalSession(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "Message not found")
		return
	}
	var found int
	if err = a.Store.DB.QueryRowContext(r.Context(), "SELECT 1 FROM external_messages WHERE id=? AND app_id=? AND recipient=?", id, s.app.id, s.recipient).Scan(&found); err != nil {
		failure(w, 404, "not_found", "Message not found")
		return
	}
	if read {
		_, err = a.Store.DB.ExecContext(r.Context(), "INSERT OR IGNORE INTO external_read_states(app_id,recipient,message_id,read_at) VALUES(?,?,?,?)", s.app.id, s.recipient, id, now())
	} else {
		_, err = a.Store.DB.ExecContext(r.Context(), "DELETE FROM external_read_states WHERE app_id=? AND recipient=? AND message_id=?", s.app.id, s.recipient, id)
	}
	if err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"read": read})
}
