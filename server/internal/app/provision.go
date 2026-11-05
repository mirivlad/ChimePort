package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// ProvisionChannel describes a channel an on-host operator wants to create
// (or reuse) for a self-hosted integration.
type ProvisionChannel struct {
	Slug string
	Name string
}

// ProvisionSource is restricted to operators with filesystem access to /data.
// It does not expose tokens over the unauthenticated HTTP API and writes a new
// secret only to an exclusive file with mode 0600.
func (s *Store) ProvisionSource(ctx context.Context, username, sourceName, label string, channels []ProvisionChannel, tokenFile string) (int64, error) {
	if !usernamePattern.MatchString(username) || strings.TrimSpace(sourceName) == "" ||
		utf8.RuneCountInString(sourceName) > 100 || strings.TrimSpace(label) == "" ||
		len(label) > 100 || len(channels) == 0 || len(channels) > 100 ||
		strings.TrimSpace(tokenFile) == "" {
		return 0, errors.New("invalid username, source, token label, channels or output path")
	}
	for _, c := range channels {
		if !slugPattern.MatchString(c.Slug) || strings.Contains(c.Slug, "//") ||
			strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 100 {
			return 0, fmt.Errorf("invalid channel %q", c.Slug)
		}
	}
	secret, hash, err := newSecret("cp_t_")
	if err != nil {
		return 0, err
	}
	encrypted, err := s.encryptToken(secret)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var userID int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE username=? AND disabled=0", username).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("user not found or disabled: %s", username)
		}
		return 0, err
	}
	channelIDs := make([]int64, 0, len(channels))
	used := map[string]bool{}
	for _, c := range channels {
		if used[c.Slug] {
			return 0, fmt.Errorf("repeated channel %q", c.Slug)
		}
		used[c.Slug] = true
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO channels(owner_id,slug,name,description,created_at) VALUES(?,?,?,?,?)", userID, c.Slug, c.Name, "Provisioned by local operator", now()); err != nil {
			return 0, err
		}
		var id int64
		if err = tx.QueryRowContext(ctx, "SELECT id FROM channels WHERE owner_id=? AND slug=?", userID, c.Slug).Scan(&id); err != nil {
			return 0, err
		}
		channelIDs = append(channelIDs, id)
	}
	var sourceID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM sources WHERE owner_id=? AND name=? ORDER BY id LIMIT 1", userID, sourceName).Scan(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "INSERT INTO sources(owner_id,name,description,created_at) VALUES(?,?,?,?)", userID, sourceName, "Local integration", now())
		if err == nil {
			sourceID, err = result.LastInsertId()
		}
	}
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO source_tokens(source_id,secret_hash,secret_encrypted,prefix,label,created_at) VALUES(?,?,?,?,?,?)", sourceID, hash, encrypted, secret[:14], label, now())
	if err != nil {
		return 0, err
	}
	tokenID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, channelID := range channelIDs {
		if _, err = tx.ExecContext(ctx, "INSERT INTO token_channels(token_id,channel_id) VALUES(?,?)", tokenID, channelID); err != nil {
			return 0, err
		}
	}

	f, err := os.OpenFile(tokenFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("create private token output: %w", err)
	}
	fileOK := false
	defer func() {
		if !fileOK {
			_ = os.Remove(tokenFile)
		}
	}()
	if _, err = f.WriteString(secret + "\n"); err != nil {
		f.Close()
		return 0, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return 0, err
	}
	if err = f.Close(); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	fileOK = true
	return tokenID, nil
}
