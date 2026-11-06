package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store owns one SQLite writer and its stable, persisted instance identity.
type Store struct {
	DB         *sql.DB
	InstanceID string
	tokenKey   []byte
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".restore-in-progress")); err == nil {
		return nil, errors.New("restore is incomplete: do not start on a half-restored volume; restore to a new EMPTY directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("check restore state: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "chimeport.db"))
	if err != nil {
		return nil, err
	}
	// One process and one connection in M1: prevents competing writes and ensures PRAGMAs apply.
	db.SetMaxOpenConns(1)
	for _, query := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(query); err != nil {
			db.Close()
			return nil, err
		}
	}
	key, keyErr := loadTokenKey(dir)
	if keyErr != nil {
		db.Close()
		return nil, keyErr
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	var id string
	err = db.QueryRow("SELECT value FROM instance WHERE key='instance_id'").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id, err = randomHex(16)
		if err == nil {
			_, err = db.Exec("INSERT INTO instance(key,value) VALUES ('instance_id',?)", id)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, InstanceID: id, tokenKey: key}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 5 {
		return fmt.Errorf("unsupported database schema %d", version)
	}
	if version == 5 {
		return nil
	}
	if version == 4 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		statements := []string{
			"CREATE TABLE IF NOT EXISTS external_apps (id INTEGER PRIMARY KEY, slug TEXT NOT NULL UNIQUE, origin TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)",
			"CREATE TABLE IF NOT EXISTS external_messages (id INTEGER PRIMARY KEY, app_id INTEGER NOT NULL REFERENCES external_apps(id) ON DELETE CASCADE, recipient TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, priority TEXT NOT NULL, url TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)",
			"CREATE INDEX IF NOT EXISTS external_messages_recipient_idx ON external_messages(app_id,recipient,id)",
			"CREATE TABLE IF NOT EXISTS external_idempotency (app_id INTEGER NOT NULL REFERENCES external_apps(id) ON DELETE CASCADE, idem_key TEXT NOT NULL, payload_hash TEXT NOT NULL, message_id INTEGER NOT NULL REFERENCES external_messages(id) ON DELETE CASCADE, PRIMARY KEY(app_id,idem_key))",
			"CREATE TABLE IF NOT EXISTS external_tickets (ticket_hash TEXT PRIMARY KEY, app_id INTEGER NOT NULL REFERENCES external_apps(id) ON DELETE CASCADE, recipient TEXT NOT NULL, challenge TEXT NOT NULL, expires_at TEXT NOT NULL)",
			"CREATE TABLE IF NOT EXISTS external_sessions (id INTEGER PRIMARY KEY, app_id INTEGER NOT NULL REFERENCES external_apps(id) ON DELETE CASCADE, recipient TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, created_at TEXT NOT NULL)",
			"CREATE TABLE IF NOT EXISTS external_read_states (app_id INTEGER NOT NULL REFERENCES external_apps(id) ON DELETE CASCADE, recipient TEXT NOT NULL, message_id INTEGER NOT NULL REFERENCES external_messages(id) ON DELETE CASCADE, read_at TEXT NOT NULL, PRIMARY KEY(app_id,recipient,message_id))",
			"CREATE TABLE IF NOT EXISTS external_webpush (id INTEGER PRIMARY KEY, session_id INTEGER NOT NULL REFERENCES external_sessions(id) ON DELETE CASCADE, endpoint TEXT NOT NULL UNIQUE, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at TEXT NOT NULL)",
			"CREATE INDEX IF NOT EXISTS external_webpush_session_idx ON external_webpush(session_id)",
			"CREATE TABLE IF NOT EXISTS external_publication_windows (app_id INTEGER PRIMARY KEY REFERENCES external_apps(id) ON DELETE CASCADE, minute INTEGER NOT NULL, accepted INTEGER NOT NULL)",
			"PRAGMA user_version=5",
		}
		for _, q := range statements {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("migration v5: %w", err)
			}
		}
		return tx.Commit()
	}
	if version == 3 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, q := range []string{
			"CREATE TABLE webpush_subscriptions (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, endpoint TEXT NOT NULL UNIQUE, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)",
			"CREATE INDEX webpush_subscriptions_user_idx ON webpush_subscriptions(user_id)",
			"PRAGMA user_version=4",
		} {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("migration v4: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return migrate(db)
	}
	if version == 1 {
		// Retain the existing v1-to-v2 migration before applying v3.
		if _, err := db.Exec("ALTER TABLE source_tokens ADD COLUMN secret_encrypted TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("migration v2: %w", err)
		}
		if _, err := db.Exec("PRAGMA user_version=2"); err != nil {
			return err
		}
		version = 2
	}
	if version == 2 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, q := range []string{
			"CREATE INDEX messages_created_at_idx ON messages(created_at,id)",
			"CREATE TABLE publication_windows (token_id INTEGER PRIMARY KEY REFERENCES source_tokens(id) ON DELETE CASCADE, minute INTEGER NOT NULL, accepted INTEGER NOT NULL)",
			"CREATE TABLE retention_watermarks (owner_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, last_deleted_id INTEGER NOT NULL)",
			"PRAGMA user_version=3",
		} {
			if _, err := tx.Exec(q); err != nil {
				return fmt.Errorf("migration v3: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return migrate(db)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE instance (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN ('admin','user')), disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE sessions (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE channels (id INTEGER PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, slug TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(owner_id,slug))`,
		`CREATE TABLE sources (id INTEGER PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE TABLE source_tokens (id INTEGER PRIMARY KEY, source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE, secret_hash TEXT NOT NULL UNIQUE, secret_encrypted TEXT NOT NULL DEFAULT '', prefix TEXT NOT NULL, label TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', expires_at TEXT, revoked_at TEXT, created_at TEXT NOT NULL)`,
		`CREATE TABLE token_channels (token_id INTEGER NOT NULL REFERENCES source_tokens(id) ON DELETE CASCADE, channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE, PRIMARY KEY(token_id,channel_id))`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY, channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE, source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE, title TEXT NOT NULL, body TEXT NOT NULL, priority TEXT NOT NULL, url TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL)`,
		`CREATE INDEX messages_channel_id_idx ON messages(channel_id,id)`,
		`CREATE INDEX messages_created_at_idx ON messages(created_at,id)`,
		`CREATE TABLE idempotency (token_id INTEGER NOT NULL REFERENCES source_tokens(id) ON DELETE CASCADE, idem_key TEXT NOT NULL, payload_hash TEXT NOT NULL, message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE, PRIMARY KEY(token_id,idem_key))`,
		`CREATE TABLE read_states (user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE, read_at TEXT NOT NULL, PRIMARY KEY(user_id,message_id))`,
		`CREATE TABLE publication_windows (token_id INTEGER PRIMARY KEY REFERENCES source_tokens(id) ON DELETE CASCADE, minute INTEGER NOT NULL, accepted INTEGER NOT NULL)`,
		`CREATE TABLE retention_watermarks (owner_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, last_deleted_id INTEGER NOT NULL)`,
		"CREATE TABLE webpush_subscriptions (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, endpoint TEXT NOT NULL UNIQUE, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)",
		"CREATE INDEX webpush_subscriptions_user_idx ON webpush_subscriptions(user_id)",
		`PRAGMA user_version=4`,
	}
	for _, q := range statements {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("migration v4 initial: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return migrate(db)
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Token values are encrypted for future owner-only retrieval, while the SHA-256 hash
// remains the lookup key for publishing. The key lives alongside SQLite in /data:
// a full backup must include both. A database-only dump does not expose tokens.
func loadTokenKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "token-key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		f, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr == nil {
			key = make([]byte, 32)
			if _, createErr = io.ReadFull(rand.Reader, key); createErr == nil {
				_, createErr = f.Write(key)
			}
			closeErr := f.Close()
			if createErr == nil {
				createErr = closeErr
			}
			if createErr != nil {
				return nil, createErr
			}
		} else if errors.Is(createErr, os.ErrExist) {
			key, err = os.ReadFile(path)
		} else {
			return nil, createErr
		}
	}
	if err != nil && len(key) == 0 {
		return nil, fmt.Errorf("read encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("invalid encryption key length; restore token-key from the same backup")
	}
	return key, nil
}
func (s *Store) encryptToken(secret string) (string, error) {
	block, err := aes.NewCipher(s.tokenKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
func (s *Store) decryptToken(encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.tokenKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("token ciphertext is truncated")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}
func (s *Store) getTokenSecret(tokenID int64) (string, error) {
	var encrypted string
	err := s.DB.QueryRow("SELECT secret_encrypted FROM source_tokens WHERE id=? AND revoked_at IS NULL", tokenID).Scan(&encrypted)
	if err != nil {
		return "", err
	}
	if encrypted == "" {
		return "", errors.New("legacy token has no recoverable secret; issue a replacement")
	}
	return s.decryptToken(encrypted)
}
