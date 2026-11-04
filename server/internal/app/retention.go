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
