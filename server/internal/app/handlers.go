package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_/-]{0,79}$`)
var priorityValues = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Mode     string `json:"mode"`
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in loginInput
	if !decode(w, r, &in) {
		return
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if in.Mode == "" {
		in.Mode = "web"
	}
	if in.Mode != "web" && in.Mode != "api" {
		failure(w, 400, "invalid_mode", "Use web or api")
		return
	}
	if in.Mode == "web" && r.Header.Get("Origin") != a.PublicURL {
		failure(w, 403, "invalid_origin", "Invalid origin")
		return
	}
	host, _, splitErr := net.SplitHostPort(r.RemoteAddr)
	if splitErr != nil {
		host = r.RemoteAddr
	}
	key := host + ":" + in.Username
	if a.limiter.blocked(key) {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "rate_limited", "Too many login attempts")
		return
	}
	var p Principal
	var hash string
	var disabled int
	err := a.Store.DB.QueryRowContext(r.Context(), "SELECT id,username,role,password_hash,disabled FROM users WHERE username=?", in.Username).Scan(&p.ID, &p.Username, &p.Role, &hash, &disabled)
	if err != nil || disabled != 0 || !verifyPassword(hash, in.Password) {
		a.limiter.record(key, false)
		failure(w, 401, "invalid_credentials", "Invalid username or password")
		return
	}
	a.limiter.record(key, true)
	token, digestValue, err := newSecret("cp_s_")
	if err != nil {
		a.err(w, err)
		return
	}
	_, err = a.Store.DB.ExecContext(r.Context(), "INSERT INTO sessions(user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?)", p.ID, digestValue, time.Now().UTC().Add(7*24*time.Hour).Format(time.RFC3339Nano), now())
	if err != nil {
		a.err(w, err)
		return
	}
	if in.Mode == "web" {
		http.SetCookie(w, sessionCookie(token, a.secure))
		respond(w, 200, map[string]any{"user": p, "expires_in": 604800})
	} else {
		respond(w, 200, map[string]any{"user": p, "access_token": token, "token_type": "Bearer", "expires_in": 604800})
	}
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	_, ok := a.authorize(w, r)
	if !ok {
		return
	}
	token := bearerToken(r)
	if token == "" {
		c, _ := r.Cookie("chimeport_session")
		token = c.Value
	}
	if _, err := a.Store.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", digest(token)); err != nil {
		a.err(w, err)
		return
	}
	c := sessionCookie("", a.secure)
	c.MaxAge = -1
	http.SetCookie(w, c)
	respond(w, 200, map[string]bool{"logged_out": true})
}
func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT id,username,role,disabled FROM users ORDER BY id")
	if err != nil {
		a.err(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id int64
		var username, role string
		var disabled bool
		if err := rows.Scan(&id, &username, &role, &disabled); err != nil {
			a.err(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "username": username, "role": role, "disabled": disabled})
	}
	if err := rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(strings.ToLower(in.Username))
	if !usernamePattern.MatchString(in.Username) || len(in.Password) < 12 {
		failure(w, 400, "invalid_user", "Username must contain 3-32 lowercase characters; password at least 12")
		return
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.Role != "user" && in.Role != "admin" {
		failure(w, 400, "invalid_role", "Invalid role")
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		a.err(w, err)
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(), "INSERT INTO users(username,password_hash,role,created_at) VALUES(?,?,?,?)", in.Username, hash, in.Role, now())
	if err != nil {
		failure(w, 409, "username_taken", "Username already exists")
		return
	}
	id, _ := result.LastInsertId()
	respond(w, 201, map[string]any{"id": id, "username": in.Username, "role": in.Role})
}
func (a *App) listChannels(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT id,slug,name,description,created_at FROM channels WHERE owner_id=? ORDER BY slug", p.ID)
	if err != nil {
		a.err(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id int64
		var slug, name, description, created string
		if err := rows.Scan(&id, &slug, &name, &description, &created); err != nil {
			a.err(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "slug": slug, "name": name, "description": description, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *App) createChannel(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	var in struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Slug = strings.TrimSpace(strings.ToLower(in.Slug))
	if !slugPattern.MatchString(in.Slug) || strings.Contains(in.Slug, "//") || utf8.RuneCountInString(in.Name) > 100 || len(strings.TrimSpace(in.Name)) == 0 || len(in.Description) > 1000 {
		failure(w, 400, "invalid_channel", "Invalid channel fields")
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(), "INSERT INTO channels(owner_id,slug,name,description,created_at) VALUES(?,?,?,?,?)", p.ID, in.Slug, in.Name, in.Description, now())
	if err != nil {
		failure(w, 409, "channel_exists", "Channel already exists")
		return
	}
	id, _ := result.LastInsertId()
	respond(w, 201, map[string]any{"id": id, "slug": in.Slug, "name": in.Name})
}
func (a *App) listSources(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), "SELECT id,name,description,created_at FROM sources WHERE owner_id=? ORDER BY id", p.ID)
	if err != nil {
		a.err(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id int64
		var name, description, created string
		if err := rows.Scan(&id, &name, &description, &created); err != nil {
			a.err(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "description": description, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *App) createSource(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > 100 || len(in.Description) > 1000 {
		failure(w, 400, "invalid_source", "Invalid source fields")
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(), "INSERT INTO sources(owner_id,name,description,created_at) VALUES(?,?,?,?)", p.ID, in.Name, in.Description, now())
	if err != nil {
		a.err(w, err)
		return
	}
	id, _ := result.LastInsertId()
	respond(w, 201, map[string]any{"id": id, "name": in.Name})
}
func (a *App) ownSource(r *http.Request, userID, sourceID int64) bool {
	var found int
	return a.Store.DB.QueryRowContext(r.Context(), "SELECT 1 FROM sources WHERE id=? AND owner_id=?", sourceID, userID).Scan(&found) == nil
}
func (a *App) listTokens(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil || !a.ownSource(r, p.ID, id) {
		failure(w, 404, "not_found", "Source not found")
		return
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT t.id,t.prefix,t.label,t.description,t.expires_at,t.revoked_at,t.created_at,COALESCE(group_concat(c.slug, ','), '') FROM source_tokens t LEFT JOIN token_channels tc ON tc.token_id=t.id LEFT JOIN channels c ON c.id=tc.channel_id WHERE t.source_id=? GROUP BY t.id ORDER BY t.id`, id)
	if err != nil {
		a.err(w, err)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id int64
		var prefix, label, description, created, allowedSlugs string
		var expires, revoked sql.NullString
		if err := rows.Scan(&id, &prefix, &label, &description, &expires, &revoked, &created, &allowedSlugs); err != nil {
			a.err(w, err)
			return
		}
		allowedChannels := []string{}
		if allowedSlugs != "" {
			allowedChannels = strings.Split(allowedSlugs, ",")
		}
		items = append(items, map[string]any{"id": id, "prefix": prefix, "label": label, "description": description, "expires_at": nullable(expires), "revoked_at": nullable(revoked), "created_at": created, "allowed_channels": allowedChannels})
	}
	if err := rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func nullable(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}
func (a *App) createToken(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	sourceID, err := parseID(r.PathValue("id"))
	if err != nil || !a.ownSource(r, p.ID, sourceID) {
		failure(w, 404, "not_found", "Source not found")
		return
	}
	var in struct {
		Label       string  `json:"label"`
		Description string  `json:"description"`
		ChannelIDs  []int64 `json:"channel_ids"`
		ExpiresAt   string  `json:"expires_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Label) == "" || len(in.Label) > 100 || len(in.Description) > 1000 || len(in.ChannelIDs) == 0 || len(in.ChannelIDs) > 100 {
		failure(w, 400, "invalid_token", "Label and 1-100 allowed channels required")
		return
	}
	var expires any = nil
	if in.ExpiresAt != "" {
		t, e := time.Parse(time.RFC3339, in.ExpiresAt)
		if e != nil || !t.After(time.Now()) {
			failure(w, 400, "invalid_expiry", "Expiry must be a future RFC3339 timestamp")
			return
		}
		expires = t.UTC().Format(time.RFC3339Nano)
	}
	token, hashed, err := newSecret("cp_t_")
	if err != nil {
		a.err(w, err)
		return
	}
	encrypted, err := a.Store.encryptToken(token)
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
	for _, channelID := range in.ChannelIDs {
		var found int
		if err := tx.QueryRow("SELECT 1 FROM channels WHERE id=? AND owner_id=?", channelID, p.ID).Scan(&found); err != nil {
			failure(w, 400, "invalid_channels", "All channel IDs must belong to you")
			return
		}
	}
	result, err := tx.Exec("INSERT INTO source_tokens(source_id,secret_hash,secret_encrypted,prefix,label,description,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?)", sourceID, hashed, encrypted, token[:14], in.Label, in.Description, expires, now())
	if err != nil {
		a.err(w, err)
		return
	}
	tokenID, _ := result.LastInsertId()
	for _, channelID := range in.ChannelIDs {
		if _, err := tx.Exec("INSERT OR IGNORE INTO token_channels(token_id,channel_id) VALUES(?,?)", tokenID, channelID); err != nil {
			a.err(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 201, map[string]any{"id": tokenID, "token": token, "prefix": token[:14], "label": in.Label, "channel_ids": in.ChannelIDs, "expires_at": expires})
}
// updateTokenPermissions atomically replaces the channels a publication token can access.
// The secret and token identity remain unchanged, so existing integrations keep working.
func (a *App) updateTokenPermissions(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	sourceID, e1 := parseID(r.PathValue("id"))
	tokenID, e2 := parseID(r.PathValue("token_id"))
	if e1 != nil || e2 != nil || !a.ownSource(r, p.ID, sourceID) {
		failure(w, 404, "not_found", "Token not found")
		return
	}
	var in struct {
		ChannelIDs []int64 `json:"channel_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.ChannelIDs) < 1 || len(in.ChannelIDs) > 100 {
		failure(w, 400, "invalid_channels", "Select 1-100 allowed channels")
		return
	}
	seen := make(map[int64]bool, len(in.ChannelIDs))
	for _, id := range in.ChannelIDs {
		if id <= 0 || seen[id] {
			failure(w, 400, "invalid_channels", "Channel IDs must be positive and unique")
			return
		}
		seen[id] = true
	}
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	var found int
	err = tx.QueryRowContext(r.Context(), "SELECT 1 FROM source_tokens WHERE id=? AND source_id=? AND revoked_at IS NULL", tokenID, sourceID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "not_found", "Active token not found")
		return
	}
	if err != nil {
		a.err(w, err)
		return
	}
	for _, channelID := range in.ChannelIDs {
		err = tx.QueryRowContext(r.Context(), "SELECT 1 FROM channels WHERE id=? AND owner_id=?", channelID, p.ID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			failure(w, 400, "invalid_channels", "All channels must belong to you")
			return
		}
		if err != nil {
			a.err(w, err)
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM token_channels WHERE token_id=?", tokenID); err != nil {
		a.err(w, err)
		return
	}
	for _, channelID := range in.ChannelIDs {
		if _, err = tx.ExecContext(r.Context(), "INSERT INTO token_channels(token_id,channel_id) VALUES(?,?)", tokenID, channelID); err != nil {
			a.err(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": tokenID, "channel_ids": in.ChannelIDs, "updated": true})
}

func (a *App) viewToken(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	sourceID, e1 := parseID(r.PathValue("id"))
	tokenID, e2 := parseID(r.PathValue("token_id"))
	if e1 != nil || e2 != nil || !a.ownSource(r, p.ID, sourceID) {
		failure(w, 404, "not_found", "Token not found")
		return
	}
	var encrypted string
	err := a.Store.DB.QueryRowContext(r.Context(), "SELECT secret_encrypted FROM source_tokens WHERE id=? AND source_id=? AND revoked_at IS NULL", tokenID, sourceID).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		failure(w, 404, "not_found", "Token not found")
		return
	}
	if err != nil {
		a.err(w, err)
		return
	}
	if encrypted == "" {
		failure(w, 409, "legacy_token", "This old token cannot be shown; create a replacement")
		return
	}
	secret, err := a.Store.decryptToken(encrypted)
	if err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]string{"token": secret})
}
func (a *App) revokeToken(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	sourceID, e1 := parseID(r.PathValue("id"))
	tokenID, e2 := parseID(r.PathValue("token_id"))
	if e1 != nil || e2 != nil || !a.ownSource(r, p.ID, sourceID) {
		failure(w, 404, "not_found", "Token not found")
		return
	}
	result, err := a.Store.DB.ExecContext(r.Context(), "UPDATE source_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND source_id=?", now(), tokenID, sourceID)
	if err != nil {
		a.err(w, err)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		failure(w, 404, "not_found", "Token not found")
		return
	}
	respond(w, 200, map[string]bool{"revoked": true})
}

type publishInput struct {
	Channel  string   `json:"channel"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Priority string   `json:"priority"`
	Tags     []string `json:"tags"`
	URL      string   `json:"url"`
}

func (a *App) publish(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if !strings.HasPrefix(token, "cp_t_") {
		failure(w, 401, "unauthorized", "Source token required")
		return
	}
	var tokenID, sourceID, ownerID int64
	var revoked, expires sql.NullString
	err := a.Store.DB.QueryRowContext(r.Context(), `SELECT t.id,s.id,s.owner_id,t.revoked_at,t.expires_at FROM source_tokens t JOIN sources s ON s.id=t.source_id JOIN users u ON u.id=s.owner_id WHERE t.secret_hash=? AND u.disabled=0`, digest(token)).Scan(&tokenID, &sourceID, &ownerID, &revoked, &expires)
	if err != nil || revoked.Valid || (expires.Valid && expires.String <= now()) {
		failure(w, 401, "unauthorized", "Invalid or expired source token")
		return
	}
	var in publishInput
	if !decode(w, r, &in) {
		return
	}
	if channelFromURL := r.URL.Query().Get("channel"); channelFromURL != "" {
		if in.Channel != "" && in.Channel != channelFromURL {
			failure(w, 400, "channel_mismatch", "Channel in URL and JSON body must match")
			return
		}
		in.Channel = channelFromURL
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !slugPattern.MatchString(in.Channel) || !priorityValues[in.Priority] || utf8.RuneCountInString(in.Title) > 200 || len(strings.TrimSpace(in.Title)) == 0 || len(in.Body) > 16*1024 || len(in.Tags) > 10 {
		failure(w, 400, "invalid_message", "Invalid message fields")
		return
	}
	for _, tag := range in.Tags {
		if strings.TrimSpace(tag) == "" || len(tag) > 40 {
			failure(w, 400, "invalid_tags", "Invalid tags")
			return
		}
	}
	if in.URL != "" {
		u, e := url.Parse(in.URL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			failure(w, 400, "invalid_url", "Only public HTTPS URLs supported")
			return
		}
	}
	var channelID int64
	err = a.Store.DB.QueryRowContext(r.Context(), `SELECT c.id FROM channels c JOIN token_channels tc ON tc.channel_id=c.id WHERE c.slug=? AND c.owner_id=? AND tc.token_id=?`, in.Channel, ownerID, tokenID).Scan(&channelID)
	if err != nil {
		failure(w, 403, "forbidden_channel", "Token cannot publish to this channel")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
		failure(w, 400, "invalid_idempotency_key", "Idempotency key must be at most 128 characters")
		return
	}
	canonical, _ := json.Marshal(in)
	h := sha256.Sum256(canonical)
	payloadHash := hex.EncodeToString(h[:])
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		a.err(w, err)
		return
	}
	defer tx.Rollback()
	if key != "" {
		var priorID int64
		var priorHash string
		err := tx.QueryRow("SELECT message_id,payload_hash FROM idempotency WHERE token_id=? AND idem_key=?", tokenID, key).Scan(&priorID, &priorHash)
		if err == nil {
			if priorHash != payloadHash {
				failure(w, 409, "idempotency_conflict", "Key was used for a different message")
				return
			}
			respond(w, 200, map[string]any{"id": strconv.FormatInt(priorID, 10), "channel": in.Channel, "stored": true, "duplicate": true})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			a.err(w, err)
			return
		}
	}
	created := now()
	result, err := tx.Exec("INSERT INTO messages(channel_id,source_id,title,body,priority,url,tags,created_at) VALUES(?,?,?,?,?,?,?,?)", channelID, sourceID, in.Title, in.Body, in.Priority, in.URL, string(canonicalTags(in.Tags)), created)
	if err != nil {
		a.err(w, err)
		return
	}
	messageID, _ := result.LastInsertId()
	if key != "" {
		if _, err := tx.Exec("INSERT INTO idempotency(token_id,idem_key,payload_hash,message_id) VALUES(?,?,?,?)", tokenID, key, payloadHash, messageID); err != nil {
			a.err(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		a.err(w, err)
		return
	}
	a.signal(ownerID, messageID)
	w.Header().Set("Location", "/api/v1/messages/"+strconv.FormatInt(messageID, 10))
	respond(w, 201, map[string]any{"id": strconv.FormatInt(messageID, 10), "channel": in.Channel, "created_at": created, "stored": true})
}
func canonicalTags(tags []string) []byte {
	if tags == nil {
		tags = []string{}
	}
	v, _ := json.Marshal(tags)
	return v
}
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	limit := 100
	if text := r.URL.Query().Get("limit"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 200 {
			failure(w, 400, "invalid_limit", "Limit must be 1-200")
			return
		}
		limit = n
	}
	last := int64(0)
	if c := r.URL.Query().Get("after"); c != "" {
		var err error
		last, err = a.parseCursor(p.ID, c)
		if err != nil {
			failure(w, 400, "invalid_cursor", "Invalid cursor")
			return
		}
	}
	rows, err := a.Store.DB.QueryContext(r.Context(), `SELECT m.id,c.id,c.slug,s.name,m.title,m.body,m.priority,m.url,m.tags,m.created_at,rs.read_at FROM messages m JOIN channels c ON c.id=m.channel_id JOIN sources s ON s.id=m.source_id LEFT JOIN read_states rs ON rs.message_id=m.id AND rs.user_id=? WHERE c.owner_id=? AND m.id>? ORDER BY m.id ASC LIMIT ?`, p.ID, p.ID, last, limit+1)
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
		var messageID, channelID int64
		var channel, source, title, body, priority, messageURL, tags, created string
		var readAt sql.NullString
		if err := rows.Scan(&messageID, &channelID, &channel, &source, &title, &body, &priority, &messageURL, &tags, &created, &readAt); err != nil {
			a.err(w, err)
			return
		}
		var parsedTags []string
		_ = json.Unmarshal([]byte(tags), &parsedTags)
		items = append(items, map[string]any{"id": strconv.FormatInt(messageID, 10), "channel_id": channelID, "channel": channel, "source": source, "title": title, "body": body, "priority": priority, "url": messageURL, "tags": parsedTags, "created_at": created, "read_at": nullable(readAt)})
		last = messageID
	}
	if err := rows.Err(); err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items, "next_cursor": a.cursor(p.ID, last), "has_more": more, "retention_gap": false})
}
func (a *App) markRead(w http.ResponseWriter, r *http.Request) {
	p, ok := a.authorize(w, r)
	if !ok {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		failure(w, 404, "not_found", "Message not found")
		return
	}
	var found int
	err = a.Store.DB.QueryRowContext(r.Context(), "SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id WHERE m.id=? AND c.owner_id=?", id, p.ID).Scan(&found)
	if err != nil {
		failure(w, 404, "not_found", "Message not found")
		return
	}
	_, err = a.Store.DB.ExecContext(r.Context(), "INSERT OR IGNORE INTO read_states(user_id,message_id,read_at) VALUES(?,?,?)", p.ID, id, now())
	if err != nil {
		a.err(w, err)
		return
	}
	respond(w, 200, map[string]bool{"read": true})
}
