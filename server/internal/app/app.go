package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webAssets embed.FS

type App struct {
	Store                *Store
	PublicURL            string
	PublishRatePerMinute int
	secure               bool
	cursorSecret         string
	vapidPublic          string
	pushSlots            chan struct{}
	pushSend             pushSendFunc
	limiter              loginLimiter
	mu                   sync.Mutex
	listeners            map[int64]map[chan int64]struct{}
	externalListeners    map[string]map[chan int64]struct{}
}

func New(store *Store, publicURL string) (*App, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("CHIMEPORT_PUBLIC_URL must be an origin without path, query or userinfo")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "[::1]")) {
		return nil, errors.New("public URL must use HTTPS (HTTP only for localhost)")
	}
	var secret string
	err = store.DB.QueryRow("SELECT value FROM instance WHERE key='cursor_secret'").Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		secret, err = randomHex(32)
		if err == nil {
			_, err = store.DB.Exec("INSERT INTO instance(key,value) VALUES('cursor_secret',?)", secret)
		}
	}
	if err != nil {
		return nil, err
	}
	a := &App{Store: store, PublicURL: publicURL, PublishRatePerMinute: 120, secure: u.Scheme == "https", cursorSecret: secret, listeners: map[int64]map[chan int64]struct{}{}, externalListeners: map[string]map[chan int64]struct{}{}}
	if err := a.initWebPush(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Store.DB.PingContext(r.Context()); err != nil {
			failure(w, 503, "db_unavailable", "Database unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/instance", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"instance_id": a.Store.InstanceID, "api_version": 1, "name": "ChimePort"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.HandleFunc("POST /api/v1/me/change-password", a.changePassword)
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.authorize(w, r)
		if ok {
			respond(w, 200, p)
		}
	})
	mux.HandleFunc("GET /api/v1/admin/users", a.listUsers)
	mux.HandleFunc("POST /api/v1/admin/users", a.createUser)
	mux.HandleFunc("POST /api/v1/admin/users/{id}/reset-password", a.resetUserPassword)
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}", a.setUserDisabled)
	mux.HandleFunc("GET /api/v1/channels", a.listChannels)
	mux.HandleFunc("POST /api/v1/channels", a.createChannel)
	mux.HandleFunc("PATCH /api/v1/channels/{id}", a.editChannel)
	mux.HandleFunc("GET /api/v1/sources", a.listSources)
	mux.HandleFunc("POST /api/v1/sources", a.createSource)
	mux.HandleFunc("PATCH /api/v1/sources/{id}", a.editSource)
	mux.HandleFunc("GET /api/v1/sources/{id}/tokens", a.listTokens)
	mux.HandleFunc("POST /api/v1/sources/{id}/tokens", a.createToken)
	mux.HandleFunc("PATCH /api/v1/sources/{id}/tokens/{token_id}", a.updateTokenPermissions)
	mux.HandleFunc("GET /api/v1/sources/{id}/tokens/{token_id}/secret", a.viewToken)
	mux.HandleFunc("DELETE /api/v1/sources/{id}/tokens/{token_id}", a.revokeToken)
	mux.HandleFunc("GET /api/v1/admin/external-apps", a.listExternalApps)
	mux.HandleFunc("POST /api/v1/admin/external-apps", a.createExternalApp)
	mux.HandleFunc("PATCH /api/v1/admin/external-apps/{id}", a.changeExternalApp)
	mux.HandleFunc("POST /api/v1/external/messages", a.publishExternal)
	mux.HandleFunc("GET /api/v1/external/messages", a.historyExternal)
	mux.HandleFunc("GET /api/v1/external/events", a.externalEvents)
	mux.HandleFunc("POST /api/v1/external/messages/{id}/read", a.markExternalRead)
	mux.HandleFunc("DELETE /api/v1/external/messages/{id}/read", a.markExternalUnread)
	mux.HandleFunc("POST /api/v1/external/tickets", a.createExternalTicket)
	mux.HandleFunc("POST /api/v1/external/exchange", a.exchangeExternalTicket)
	mux.HandleFunc("POST /api/v1/external/logout", a.externalLogout)
	mux.HandleFunc("GET /api/v1/external/info", a.externalInfo)
	mux.HandleFunc("GET /api/v1/external/webpush/vapid-public-key", a.externalVapidPublic)
	mux.HandleFunc("POST /api/v1/external/webpush/subscriptions", a.externalWebPushSubscribe)
	mux.HandleFunc("DELETE /api/v1/external/webpush/subscriptions", a.externalWebPushUnsubscribe)
	mux.HandleFunc("POST /api/v1/messages", a.publish)
	mux.HandleFunc("GET /api/v1/messages", a.history)
	mux.HandleFunc("POST /api/v1/messages/{id}/read", a.markRead)
	mux.HandleFunc("DELETE /api/v1/messages/{id}/read", a.markUnread)
	mux.HandleFunc("GET /api/v1/events", a.events)
	mux.HandleFunc("GET /api/v1/webpush/vapid-public-key", a.webPushPublicKey)
	mux.HandleFunc("GET /api/v1/webpush/subscriptions", a.webPushSubscriptions)
	mux.HandleFunc("POST /api/v1/webpush/subscriptions", a.webPushSubscribe)
	mux.HandleFunc("DELETE /api/v1/webpush/subscriptions", a.webPushUnsubscribe)
	mux.HandleFunc("GET /", a.web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'")
		if a.secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *App) web(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" && r.URL.Path != "/examples" && r.URL.Path != "/examples.js" && r.URL.Path != "/themes.css" && r.URL.Path != "/theme.js" && r.URL.Path != "/chimeport-icon.svg" && r.URL.Path != "/sw.js" && r.URL.Path != "/push.js" && r.URL.Path != "/manifest.webmanifest" && r.URL.Path != "/icons/icon-192.png" && r.URL.Path != "/icons/icon-512.png" {
		http.NotFound(w, r)
		return
	}
	path := "web/index.html"
	mime := "text/html; charset=utf-8"
	if r.URL.Path == "/app.js" {
		path = "web/app.js"
		mime = "text/javascript; charset=utf-8"
	}
	if r.URL.Path == "/style.css" {
		path = "web/style.css"
		mime = "text/css; charset=utf-8"
	}
	if r.URL.Path == "/themes.css" {
		path = "web/themes.css"
		mime = "text/css; charset=utf-8"
	}
	if r.URL.Path == "/theme.js" {
		path = "web/theme.js"
		mime = "text/javascript; charset=utf-8"
	}
	if r.URL.Path == "/chimeport-icon.svg" {
		path = "web/chimeport-icon.svg"
		mime = "image/svg+xml"
	}
	if r.URL.Path == "/sw.js" {
		path = "web/sw.js"
		mime = "text/javascript; charset=utf-8"
	}
	if r.URL.Path == "/push.js" {
		path = "web/push.js"
		mime = "text/javascript; charset=utf-8"
	}
	if r.URL.Path == "/manifest.webmanifest" {
		path = "web/manifest.webmanifest"
		mime = "application/manifest+json"
	}
	if r.URL.Path == "/icons/icon-192.png" {
		path = "web/icon-192.png"
		mime = "image/png"
	}
	if r.URL.Path == "/icons/icon-512.png" {
		path = "web/icon-512.png"
		mime = "image/png"
	}
	if r.URL.Path == "/examples" {
		path = "web/examples.html"
	}
	if r.URL.Path == "/examples.js" {
		path = "web/examples.js"
		mime = "text/javascript; charset=utf-8"
	}
	content, err := webAssets.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(content)
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code, msg string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		failure(w, 415, "unsupported_media_type", "Use application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		failure(w, 400, "invalid_json", "Invalid JSON request")
		return false
	}
	var rest any
	if err := decoder.Decode(&rest); err != io.EOF {
		failure(w, 400, "invalid_json", "Only one JSON value is allowed")
		return false
	}
	return true
}
func (a *App) err(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	failure(w, 500, "internal_error", "Internal server error")
}
func (a *App) cursor(userID, last int64) string {
	data := fmt.Sprintf("%s:%d:%d", a.Store.InstanceID, userID, last)
	m := hmac.New(sha256.New, []byte(a.cursorSecret))
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString([]byte(data)) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (a *App) parseCursor(userID int64, c string) (int64, error) {
	parts := strings.Split(c, ".")
	if len(parts) != 2 {
		return 0, errors.New("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	m := hmac.New(sha256.New, []byte(a.cursorSecret))
	m.Write(data)
	if !hmac.Equal(signature, m.Sum(nil)) {
		return 0, errors.New("invalid cursor signature")
	}
	segments := strings.Split(string(data), ":")
	if len(segments) != 3 || segments[0] != a.Store.InstanceID || segments[1] != strconv.FormatInt(userID, 10) {
		return 0, errors.New("cursor belongs to another account or server")
	}
	last, err := strconv.ParseInt(segments[2], 10, 64)
	if err != nil || last < 0 {
		return 0, errors.New("invalid cursor sequence")
	}
	return last, nil
}
func (a *App) signal(userID, last int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for ch := range a.listeners[userID] {
		select {
		case ch <- last:
		default:
		}
	}
}
func (a *App) events(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		failure(w, 500, "unsupported", "SSE unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	stream := make(chan int64, 16)
	a.mu.Lock()
	if a.listeners[p.ID] == nil {
		a.listeners[p.ID] = map[chan int64]struct{}{}
	}
	a.listeners[p.ID][stream] = struct{}{}
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.listeners[p.ID], stream); a.mu.Unlock() }()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case id := <-stream:
			fmt.Fprintf(w, "event: messages_available\ndata: {\"last_id\":\"%d\"}\n\n", id)
			flusher.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
