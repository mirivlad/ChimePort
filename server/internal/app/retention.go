package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PruneExpired removes a bounded batch of expired messages and their dependent
// idempotency/read-state rows (via foreign-key cascades). The owner-specific
// watermark allows sync clients to detect a retention gap.
func (s *Store) PruneExpired(ctx context.Context, days, batchSize int) (int, error) {
	if days < 0 || batchSize <= 0 || batchSize > 900 {
		return 0, fmt.Errorf("invalid retention days or batch size")
	}
	if days == 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Truncate(time.Second).Format("2006-01-02T15:04:05")
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT m.id,c.owner_id FROM messages m JOIN channels c ON c.id=m.channel_id WHERE m.created_at<? ORDER BY m.created_at,m.id LIMIT ?", cutoff, batchSize)
	if err != nil {
		return 0, err
	}
	ids := make([]int64, 0, batchSize)
	watermarks := map[int64]int64{}
	for rows.Next() {
		var id, ownerID int64
		if err := rows.Scan(&id, &ownerID); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		if id > watermarks[ownerID] {
			watermarks[ownerID] = id
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, tx.Commit()
	}
	for owner, id := range watermarks {
		if _, err := tx.ExecContext(ctx, "INSERT INTO retention_watermarks(owner_id,last_deleted_id) VALUES(?,?) ON CONFLICT(owner_id) DO UPDATE SET last_deleted_id=MAX(retention_watermarks.last_deleted_id,excluded.last_deleted_id)", owner, id); err != nil {
			return 0, err
		}
	}
	params := make([]any, len(ids))
	for i, id := range ids {
		params[i] = id
	}
	query := "DELETE FROM messages WHERE id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	if _, err := tx.ExecContext(ctx, query, params...); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// PruneExternalExpired bounds the size of trusted-application histories without
// changing their tenant-scoped semantics. Old idempotency/read state rows follow
// the message through foreign-key cascades. The caller batches by message ID.
func (s *Store) PruneExternalExpired(ctx context.Context, days, batchSize int) (int, error) {
	if days < 0 || batchSize <= 0 || batchSize > 900 {
		return 0, fmt.Errorf("invalid retention days or batch size")
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339Nano)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ids := []int64{}
	if days > 0 {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM external_messages WHERE created_at<? ORDER BY id LIMIT ?", cutoff, batchSize)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			ids = append(ids, id)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		rows.Close()
		for _, id := range ids {
			if _, err = tx.ExecContext(ctx, "DELETE FROM external_messages WHERE id=?", id); err != nil {
				return 0, err
			}
		}
	}
	// Tickets cannot be exchanged after 90 seconds. Clear stale entries as
	// part of the retention pass, even when no old messages were found.
	if _, err = tx.ExecContext(ctx, "DELETE FROM external_tickets WHERE expires_at<?", now()); err != nil {
		return 0, err
	}
	// Credential cleanup runs even when message retention is disabled.
	// Active browser Web Push grants remain valid while TMS authorizes them.
	if _, err = tx.ExecContext(ctx, "DELETE FROM external_sessions WHERE revoked_at IS NOT NULL AND revoked_at<?", time.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx,
		"DELETE FROM external_sessions WHERE refresh_expires_at<? AND id NOT IN (SELECT session_id FROM external_webpush)",
		now()); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM external_used_refresh WHERE used_at<?",
		time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}
