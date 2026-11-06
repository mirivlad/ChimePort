package app

import (
	"context"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const maxWebPushSubscriptions = 16

type pushSendFunc func(context.Context, []byte, *webpush.Subscription) (int, error)

// Keep the VAPID identity in the existing SQLite instance table. Encrypting the
// private part with token-key makes the current verified backup format complete.
func loadVAPIDKeys(s *Store) (public, private string, err error) {
	var encrypted string
	err = s.DB.QueryRow("SELECT value FROM instance WHERE key='webpush_vapid_public'").Scan(&public)
	if errors.Is(err, sql.ErrNoRows) {
		private, public, err = webpush.GenerateVAPIDKeys()
		if err != nil {
			return "", "", fmt.Errorf("generate VAPID: %w", err)
		}
		encrypted, err = s.encryptToken(private)
		if err != nil {
			return "", "", err
		}
		tx, txErr := s.DB.Begin()
		if txErr != nil {
			return "", "", txErr
		}
		defer tx.Rollback()
		for _, entry := range [][2]string{
			{"webpush_vapid_public", public},
			{"webpush_vapid_private", encrypted},
		} {
			if _, err = tx.Exec("INSERT INTO instance(key,value) VALUES(?,?)", entry[0], entry[1]); err != nil {
				return "", "", fmt.Errorf("persist VAPID: %w", err)
			}
		}
		if err = tx.Commit(); err != nil {
			return "", "", err
		}
		return public, private, nil
	}
	if err != nil {
		return "", "", err
	}
	if err = s.DB.QueryRow("SELECT value FROM instance WHERE key='webpush_vapid_private'").Scan(&encrypted); err != nil {
		return "", "", fmt.Errorf("VAPID key incomplete: restore matching database and token-key: %w", err)
	}
	private, err = s.decryptToken(encrypted)
	if err != nil {
		return "", "", fmt.Errorf("cannot decrypt VAPID key: restore token-key: %w", err)
	}
	key, err := base64.RawURLEncoding.DecodeString(public)
	if err != nil || len(key) != 65 || key[0] != 4 {
		return "", "", errors.New("stored VAPID public key is corrupt")
	}
	if x, y := elliptic.Unmarshal(elliptic.P256(), key); x == nil || y == nil {
		return "", "", errors.New("stored VAPID public key is invalid")
	}
	if _, err = base64.RawURLEncoding.DecodeString(private); err != nil {
		return "", "", errors.New("stored VAPID private key is corrupt")
	}
	return public, private, nil
}

// Push endpoints must be browser-operated gateways, never arbitrary user URLs.
// An authenticated caller must not be able to turn ChimePort into an SSRF proxy.
func allowedPushHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	switch host {
	case "fcm.googleapis.com", "fcm-xm.googleapis.com",
		"updates.push.services.mozilla.com", "web.push.apple.com":
		return true
	}
	return strings.HasSuffix(host, ".push.apple.com") ||
		strings.HasSuffix(host, ".notify.windows.com")
}

func validatePushEndpoint(raw string) error {
	if len(raw) < 16 || len(raw) > 2048 {
		return errors.New("push endpoint length invalid")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" ||
		u.User != nil || u.Fragment != "" || u.Port() != "" || u.Opaque != "" ||
		net.ParseIP(u.Hostname()) != nil || !allowedPushHost(u.Hostname()) {
		return errors.New("only supported HTTPS browser push endpoints are accepted")
	}
	return nil
}

func validateBrowserKeys(s webpush.Subscription) error {
	if err := validatePushEndpoint(s.Endpoint); err != nil {
		return err
	}
	public, err := base64.RawURLEncoding.DecodeString(s.Keys.P256dh)
	if err != nil || len(public) != 65 || public[0] != 4 {
		return errors.New("invalid browser P-256 key")
	}
	if x, y := elliptic.Unmarshal(elliptic.P256(), public); x == nil || y == nil {
		return errors.New("invalid browser public key")
	}
	auth, err := base64.RawURLEncoding.DecodeString(s.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("invalid browser auth secret")
	}
	return nil
}

// Block private/loopback/rebinding targets even when DNS resolves a permitted
// gateway name to an internal address. TLS still verifies the original host.
func dialPushGateway(ctx context.Context, network, address string) (net.Conn, error) {
	hostname, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" || !allowedPushHost(hostname) {
		return nil, errors.New("push target is not a permitted HTTPS gateway")
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	dialer.Timeout = 4 * time.Second
	for _, addr := range addrs {
		ip := addr.IP
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("push gateway has no public IP")
}

func (a *App) initWebPush() error {
	public, private, err := loadVAPIDKeys(a.Store)
	if err != nil {
		return err
	}
	a.vapidPublic = public
	a.pushSlots = make(chan struct{}, 8)
	client := &http.Client{
		Timeout: 9 * time.Second,
		Transport: &http.Transport{
			Proxy:             nil,
			DialContext:       dialPushGateway,
			ForceAttemptHTTP2: true,
			MaxIdleConns:      8,
			IdleConnTimeout:   25 * time.Second,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("push gateway redirect refused")
		},
	}
	a.pushSend = func(ctx context.Context, payload []byte, subscription *webpush.Subscription) (int, error) {
		resp, err := webpush.SendNotificationWithContext(ctx, payload, subscription, &webpush.Options{
			Subscriber:      a.PublicURL,
			VAPIDPublicKey:  public,
			VAPIDPrivateKey: private,
			TTL:             3600,
			HTTPClient:      client,
		})
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, nil
	}
	return nil
}

// /api/v1/webpush/vapid-public-key is intentionally read-only and requires a
// user session; source tokens cannot subscribe.
func (a *App) webPushPublicKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorize(w, r); !ok {
		return
	}
	respond(w, 200, map[string]string{"public_key": a.vapidPublic})
}

func (a *App) currentWebSessionID(r *http.Request, userID int64) (int64, error) {
	token := bearerToken(r)
	if token == "" {
		cookie, err := r.Cookie("chimeport_session")
		if err != nil {
			return 0, err
		}
		token = cookie.Value
	}
	var id int64
	err := a.Store.DB.QueryRowContext(r.Context(),
		"SELECT id FROM sessions WHERE user_id=? AND token_hash=? AND expires_at>?",
		userID, digest(token), now()).Scan(&id)
	return id, err
}

func (a *App) webPushSubscribe(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	var subscription webpush.Subscription
	if !decode(w, r, &subscription) {
		return
	}
	if err := validateBrowserKeys(subscription); err != nil {
		failure(w, 400, "invalid_subscription", err.Error())
		return
	}
	sessionID, err := a.currentWebSessionID(r, p.ID)
	if err != nil {
		failure(w, 401, "unauthorized", "Session expired")
		return
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	var oldID, oldOwnerID int64
	err = tx.QueryRow("SELECT id,user_id FROM webpush_subscriptions WHERE endpoint=?", subscription.Endpoint).Scan(&oldID, &oldOwnerID)
	if err == nil && oldOwnerID != p.ID {
		failure(w, 409, "subscription_in_use", "This browser endpoint belongs to another account; sign out there first")
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.err(w, err)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM webpush_subscriptions WHERE user_id=?", p.ID).Scan(&count); err != nil {
			a.err(w, err)
			return
		}
		if count >= maxWebPushSubscriptions {
			failure(w, 409, "subscription_limit", "Too many browser subscriptions; disable one on another device")
			return
		}
		_, err = tx.Exec("INSERT INTO webpush_subscriptions(user_id,session_id,endpoint,p256dh,auth,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
			p.ID, sessionID, subscription.Endpoint, subscription.Keys.P256dh, subscription.Keys.Auth, now(), now())
	} else {
		// Rebind only within the same account. Another account cannot steal
		// an existing endpoint; logout first removes the old session.
		_, err = tx.Exec("UPDATE webpush_subscriptions SET user_id=?,session_id=?,p256dh=?,auth=?,updated_at=? WHERE id=?",
			p.ID, sessionID, subscription.Keys.P256dh, subscription.Keys.Auth, now(), oldID)
	}
	if err != nil {
		a.err(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"subscribed": true})
}

func (a *App) webPushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	var in struct{ Endpoint string }
	if !decode(w, r, &in) {
		return
	}
	if len(in.Endpoint) > 2048 || in.Endpoint == "" {
		failure(w, 400, "invalid_endpoint", "Invalid endpoint")
		return
	}
	if _, err := a.Store.DB.ExecContext(r.Context(),
		"DELETE FROM webpush_subscriptions WHERE user_id=? AND endpoint=?", p.ID, in.Endpoint); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"subscribed": false})
}

func (a *App) webPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	var count int
	err := a.Store.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM webpush_subscriptions w JOIN sessions s ON s.id=w.session_id WHERE w.user_id=?",
		p.ID).Scan(&count)
	if err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]int{"count": count})
}

func (a *App) queueWebPush(userID, messageID int64, title, body, channel, priority string) {
	if a.pushSlots == nil || a.pushSend == nil {
		return
	}
	select {
	case a.pushSlots <- struct{}{}:
		go func() {
			defer func() { <-a.pushSlots }()
			a.deliverWebPush(userID, messageID, title, body, channel, priority)
		}()
	default:
		// The message is still durably stored. Web Push is best-effort.
		slog.Warn("web push worker saturated; message saved for later sync")
	}
}

func trimPushText(value string, max int) string {
	chars := []rune(value)
	if len(chars) > max {
		return string(chars[:max]) + "…"
	}
	return value
}

// Classify failures without logging the full push endpoint URL (a bearer secret).
func webPushErrorClass(err error) string {
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var tlsVerify *tls.CertificateVerificationError
	var dnsErr *net.DNSError
	var networkErr *net.OpError
	var transportErr net.Error
	switch {
	case errors.As(err, &authority):
		return "tls_unknown_authority"
	case errors.As(err, &invalid), errors.As(err, &tlsVerify):
		return "tls_verify_failed"
	case errors.As(err, &dnsErr):
		return "dns_error"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.As(err, &transportErr) && transportErr.Timeout():
		return "network_timeout"
	case errors.As(err, &networkErr):
		return "network_error"
	default:
		return "other_error"
	}
}

func (a *App) deliverWebPush(userID, messageID int64, title, body, channel, priority string) {
	rows, err := a.Store.DB.Query(
		"SELECT w.id,w.endpoint,w.p256dh,w.auth FROM webpush_subscriptions w JOIN sessions s ON s.id=w.session_id JOIN users u ON u.id=w.user_id WHERE w.user_id=? AND u.disabled=0 ORDER BY w.id LIMIT ?",
		userID, maxWebPushSubscriptions)
	if err != nil {
		slog.Error("web push lookup failed", "error", err)
		return
	}
	type target struct {
		ID           int64
		Subscription webpush.Subscription
	}
	var targets []target
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.ID, &t.Subscription.Endpoint, &t.Subscription.Keys.P256dh, &t.Subscription.Keys.Auth); err != nil {
			break
		}
		targets = append(targets, t)
	}
	if scanErr := rows.Err(); scanErr != nil {
		err = scanErr
	}
	rows.Close()
	if err != nil {
		slog.Error("web push lookup failed", "error", err)
		return
	}

	payload, err := json.Marshal(map[string]string{
		"id": fmt.Sprint(messageID), "title": trimPushText(title, 120),
		"body": trimPushText(body, 300), "channel": trimPushText(channel, 80),
		"priority": priority,
	})
	if err != nil {
		return
	}
	for _, target := range targets {
		if validateBrowserKeys(target.Subscription) != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		status, sendErr := a.pushSend(ctx, payload, &target.Subscription)
		cancel()
		if sendErr != nil {
			slog.Warn("web push delivery failed", "subscription_id", target.ID, "error_class", webPushErrorClass(sendErr))
			continue
		}
		if status == 404 || status == 410 {
			// Expired endpoints must not be retried. Guard the endpoint in case
			// the row was refreshed between the send and this deletion.
			if _, err := a.Store.DB.Exec(
				"DELETE FROM webpush_subscriptions WHERE id=? AND endpoint=?", target.ID, target.Subscription.Endpoint); err != nil {
				slog.Error("web push cleanup failed", "error", err)
			}
		} else if status < 200 || status >= 300 {
			slog.Warn("web push gateway refused delivery", "subscription_id", target.ID, "status", status)
		}
	}
}
