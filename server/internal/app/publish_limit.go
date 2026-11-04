package app

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// consumePublishQuota is transactional with message insertion. Rejected writes
// never consume quota; a replay of an existing Idempotency-Key bypasses it.
func (a *App) consumePublishQuota(ctx context.Context, tx *sql.Tx, tokenID int64) (int, error) {
	if a.PublishRatePerMinute == 0 {
		return 0, nil
	}
	seconds := time.Now().UTC().Unix()
	minute := seconds / 60
	var recordedMinute int64
	var accepted int
	err := tx.QueryRowContext(ctx, "SELECT minute,accepted FROM publication_windows WHERE token_id=?", tokenID).Scan(&recordedMinute, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "INSERT INTO publication_windows(token_id,minute,accepted) VALUES(?,?,1)", tokenID, minute)
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	if recordedMinute != minute {
		_, err = tx.ExecContext(ctx, "UPDATE publication_windows SET minute=?,accepted=1 WHERE token_id=?", minute, tokenID)
		return 0, err
	}
	if accepted >= a.PublishRatePerMinute {
		return int(60 - seconds%60), nil
	}
	_, err = tx.ExecContext(ctx, "UPDATE publication_windows SET accepted=accepted+1 WHERE token_id=?", tokenID)
	return 0, err
}
