package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"telesrv/internal/domain"
	"telesrv/internal/store"
	"telesrv/internal/store/postgres/sqlcgen"
)

// BroadcastStore persists system broadcast campaigns (see
// deploy/migrations/20260714003131_system_broadcasts.up.sql).
type BroadcastStore struct {
	db sqlcgen.DBTX
}

// NewBroadcastStore builds the store on a pgx pool or transaction.
func NewBroadcastStore(db sqlcgen.DBTX) *BroadcastStore {
	return &BroadcastStore{db: db}
}

var _ store.BroadcastStore = (*BroadcastStore)(nil)

// CreateBroadcast inserts the broadcast row and one pending recipient row per
// id, deduplicating recipientUserIDs (a "selected" list built by hand in the
// panel could otherwise carry a repeat) via ON CONFLICT DO NOTHING against
// the (broadcast_id, user_id) unique constraint.
func (s *BroadcastStore) CreateBroadcast(ctx context.Context, message string, targetMode domain.BroadcastTargetMode, recipientUserIDs []int64, createdBy string) (domain.Broadcast, error) {
	if len(recipientUserIDs) == 0 {
		return domain.Broadcast{}, domain.ErrBroadcastNoRecipients
	}
	var out domain.Broadcast
	err := withTx(ctx, s.db, "create broadcast", func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
INSERT INTO broadcasts (message, target_mode, total_count, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id, message, target_mode, total_count, created_by, created_at`,
			message, string(targetMode), len(recipientUserIDs), createdBy,
		).Scan(&out.ID, &out.Message, &out.TargetMode, &out.TotalCount, &out.CreatedBy, &out.CreatedAt); err != nil {
			return fmt.Errorf("insert broadcast: %w", err)
		}
		batch := &pgx.Batch{}
		for _, userID := range recipientUserIDs {
			batch.Queue(`
INSERT INTO broadcast_recipients (broadcast_id, user_id)
VALUES ($1, $2)
ON CONFLICT (broadcast_id, user_id) DO NOTHING`, out.ID, userID)
		}
		results := tx.SendBatch(ctx, batch)
		defer results.Close()
		for range recipientUserIDs {
			if _, err := results.Exec(); err != nil {
				return fmt.Errorf("insert broadcast recipient: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.Broadcast{}, err
	}
	return out, nil
}

// PendingBroadcastRecipients returns undelivered outbox rows, oldest first,
// each carrying its broadcast's message text.
func (s *BroadcastStore) PendingBroadcastRecipients(ctx context.Context, limit int) ([]store.PendingBroadcastRecipient, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
SELECT r.id, r.broadcast_id, r.user_id, r.attempts, b.message
FROM broadcast_recipients r
JOIN broadcasts b ON b.id = r.broadcast_id
WHERE r.status = 'pending'
ORDER BY r.id
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending broadcast recipients: %w", err)
	}
	defer rows.Close()
	out := make([]store.PendingBroadcastRecipient, 0, limit)
	for rows.Next() {
		var item store.PendingBroadcastRecipient
		if err := rows.Scan(&item.RecipientID, &item.BroadcastID, &item.UserID, &item.Attempts, &item.Message); err != nil {
			return nil, fmt.Errorf("scan pending broadcast recipient: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending broadcast recipients: %w", err)
	}
	return out, nil
}

// MarkBroadcastRecipientSent closes a recipient row as delivered. Closing an
// already-closed row is a no-op: the outbox is exactly-once, not
// at-least-once.
func (s *BroadcastStore) MarkBroadcastRecipientSent(ctx context.Context, recipientID int64) error {
	if _, err := s.db.Exec(ctx, `
UPDATE broadcast_recipients
SET status = 'sent', sent_at = now(), last_error = ''
WHERE id = $1 AND status = 'pending'`, recipientID); err != nil {
		return fmt.Errorf("mark broadcast recipient sent: %w", err)
	}
	return nil
}

// MarkBroadcastRecipientFailed records a failed delivery attempt. The row
// stays 'pending' (retried on the next cycle) until attempts reaches
// domain.MaxBroadcastRecipientAttempts, at which point it becomes the
// terminal 'failed' status so a permanently blocked/deleted recipient
// doesn't spin forever alongside real deliveries.
func (s *BroadcastStore) MarkBroadcastRecipientFailed(ctx context.Context, recipientID int64, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	if _, err := s.db.Exec(ctx, `
UPDATE broadcast_recipients
SET attempts = attempts + 1,
    last_error = $2,
    status = CASE WHEN attempts + 1 >= $3 THEN 'failed' ELSE 'pending' END
WHERE id = $1 AND status = 'pending'`, recipientID, reason, domain.MaxBroadcastRecipientAttempts); err != nil {
		return fmt.Errorf("mark broadcast recipient failed: %w", err)
	}
	return nil
}

const broadcastSelectColumns = `
	b.id, b.message, b.target_mode, b.total_count, b.created_by, b.created_at,
	count(*) FILTER (WHERE r.status = 'sent')::int AS sent_count,
	count(*) FILTER (WHERE r.status = 'failed')::int AS failed_count`

func scanBroadcastRow(row interface{ Scan(...any) error }, item *domain.Broadcast) error {
	return row.Scan(&item.ID, &item.Message, &item.TargetMode, &item.TotalCount, &item.CreatedBy, &item.CreatedAt,
		&item.SentCount, &item.FailedCount)
}

// ListBroadcasts pages broadcasts newest-first, each with sent/failed counts
// derived live from its recipient rows (never stored, so they can't drift).
func (s *BroadcastStore) ListBroadcasts(ctx context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `
SELECT `+broadcastSelectColumns+`
FROM broadcasts b
LEFT JOIN broadcast_recipients r ON r.broadcast_id = b.id
WHERE $1::bigint = 0 OR b.id < $1
GROUP BY b.id
ORDER BY b.id DESC
LIMIT $2`, beforeID, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list broadcasts: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Broadcast, 0, limit+1)
	for rows.Next() {
		var item domain.Broadcast
		if err := scanBroadcastRow(rows, &item); err != nil {
			return nil, false, fmt.Errorf("scan broadcast: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate broadcasts: %w", err)
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// BroadcastByID returns one broadcast with derived counts.
func (s *BroadcastStore) BroadcastByID(ctx context.Context, id int64) (domain.Broadcast, bool, error) {
	var item domain.Broadcast
	err := scanBroadcastRow(s.db.QueryRow(ctx, `
SELECT `+broadcastSelectColumns+`
FROM broadcasts b
LEFT JOIN broadcast_recipients r ON r.broadcast_id = b.id
WHERE b.id = $1
GROUP BY b.id`, id), &item)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.Broadcast{}, false, nil
		}
		return domain.Broadcast{}, false, fmt.Errorf("get broadcast: %w", err)
	}
	return item, true, nil
}
