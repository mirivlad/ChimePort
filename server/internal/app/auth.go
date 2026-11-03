package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=4" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
func newSecret(prefix string) (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	secret := prefix + base64.RawURLEncoding.EncodeToString(b)
	return secret, digest(secret), nil
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type Principal struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (a *App) principal(r *http.Request) (Principal, error) {
	token := bearerToken(r)
	if token == "" {
		if c, err := r.Cookie("chimeport_session"); err == nil {
			token = c.Value
		}
	}
	if !strings.HasPrefix(token, "cp_s_") {
		return Principal{}, sql.ErrNoRows
	}
	var p Principal
	err := a.Store.DB.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.role FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>? AND u.disabled=0`, digest(token), now()).Scan(&p.ID, &p.Username, &p.Role)
	return p, err
}
func bearerToken(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}
func (a *App) authorize(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, err := a.principal(r)
	if err != nil {
		failure(w, http.StatusUnauthorized, "unauthorized", "Sign in required")
		return Principal{}, false
	}
	if bearerToken(r) == "" && r.Method != "GET" && r.Method != "HEAD" {
		// Cookie-authenticated mutations require an exact same-origin Origin header.
		if r.Header.Get("Origin") != a.PublicURL {
			failure(w, http.StatusForbidden, "invalid_origin", "Cross-origin request rejected")
			return Principal{}, false
		}
	}
	return p, true
}
func (a *App) admin(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, ok := a.authorize(w, r)
	if !ok {
		return p, false
	}
	if p.Role != "admin" {
		failure(w, 403, "forbidden", "Administrator required")
		return p, false
	}
	return p, true
}

func (s *Store) Bootstrap(username, password string) error {
	username = strings.TrimSpace(strings.ToLower(username))
	if !usernamePattern.MatchString(username) || len(password) < 12 {
		return errors.New("username must be 3-32 lower-case characters; password must contain at least 12 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("bootstrap already completed; admin account exists")
	}
	if _, err := tx.Exec(`INSERT INTO users(username,password_hash,role,created_at) VALUES (?,?, 'admin',?)`, username, hash, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// In-memory limiter is an initial safeguard; a reverse proxy should add persistent IP limits.
type loginLimit struct {
	failures int
	since    time.Time
}
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]loginLimit
}

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.entries[key]
	if time.Since(v.since) > time.Minute {
		return false
	}
	return v.failures >= 5
}
func (l *loginLimiter) record(key string, success bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]loginLimit{}
	}
	if success {
		delete(l.entries, key)
		return
	}
	v := l.entries[key]
	if time.Since(v.since) > time.Minute {
		v = loginLimit{since: time.Now()}
	}
	v.failures++
	l.entries[key] = v
	if len(l.entries) > 10000 {
		l.entries = map[string]loginLimit{key: v}
	}
}
func sessionCookie(token string, secure bool) *http.Cookie {
	return &http.Cookie{Name: "chimeport_session", Value: token, Path: "/api/v1", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600}
}
func parseID(v string) (int64, error) {
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
