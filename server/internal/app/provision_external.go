package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ProvisionTrustedApplication is an explicit, privileged operator action
// requiring write access to the persistent ChimePort data directory.
// No password hashes or login names from the trusted application are stored.
// It writes the once-only app credential to a private, exclusive file and
// never exposes that secret in stdout, logs, or HTTP responses.
func (s *Store) ProvisionTrustedApplication(ctx context.Context, slug, origin, tokenFile string) (int64, error) {
	if !appSlugPattern.MatchString(slug) || !validAppOrigin(origin) {
		return 0, errors.New("invalid trusted application name or HTTPS origin")
	}
	if !filepath.IsAbs(tokenFile) || filepath.Base(tokenFile) == "." {
		return 0, errors.New("token output must be an absolute file path")
	}
	token, hash, err := newSecret("cp_a_")
	if err != nil {
		return 0, err
	}
	encrypted, err := s.encryptToken(token)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Provision only on an initialized instance with an enabled local admin.
	var admin int
	if err = tx.QueryRowContext(ctx, "SELECT 1 FROM users WHERE role='admin' AND disabled=0 LIMIT 1").Scan(&admin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("no enabled administrator exists")
		}
		return 0, err
	}
	result, err := tx.ExecContext(ctx,
		"INSERT INTO external_apps(slug,origin,token_hash,token_encrypted,created_at) VALUES(?,?,?,?,?)",
		slug, origin, hash, encrypted, now())
	if err != nil {
		return 0, fmt.Errorf("could not register trusted application: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(tokenFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("create private token file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tokenFile)
		}
	}()
	if _, err = f.WriteString(token + "\n"); err != nil {
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
	committed = true
	return id, nil
}
