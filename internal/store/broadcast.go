package store

import (
	"context"

	"telesrv/internal/domain"
)

// BroadcastStore persists system broadcast campaigns and their durable
// per-recipient delivery outbox.
type BroadcastStore interface {
	// CreateBroadcast inserts the broadcast row and one pending recipient row
	// per id in recipientUserIDs, in a single transaction: a broadcast with
	// zero recipients (an empty "selected" list, or an "all" snapshot taken
	// when there happen to be no eligible users) is rejected with
	// domain.ErrBroadcastNoRecipients rather than created empty.
	CreateBroadcast(ctx context.Context, message string, targetMode domain.BroadcastTargetMode, recipientUserIDs []int64, createdBy string) (domain.Broadcast, error)
	// PendingBroadcastRecipients returns undelivered outbox rows across every
	// broadcast, oldest first, each carrying its broadcast's message text so
	// the worker can send without a second round trip per row.
	PendingBroadcastRecipients(ctx context.Context, limit int) ([]PendingBroadcastRecipient, error)
	// MarkBroadcastRecipientSent closes a recipient row as delivered.
	MarkBroadcastRecipientSent(ctx context.Context, recipientID int64) error
	// MarkBroadcastRecipientFailed records a failed attempt. The row stays
	// 'pending' (retried on the next cycle) until attempts reaches
	// domain.MaxBroadcastRecipientAttempts, at which point it becomes the
	// terminal 'failed' status.
	MarkBroadcastRecipientFailed(ctx context.Context, recipientID int64, reason string) error
	// ListBroadcasts pages broadcasts newest-first, each with sent/failed
	// counts derived live from its recipient rows.
	ListBroadcasts(ctx context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error)
	// BroadcastByID returns one broadcast with derived counts.
	BroadcastByID(ctx context.Context, id int64) (domain.Broadcast, bool, error)
}

// PendingBroadcastRecipient is one undelivered outbox row, joined with its
// broadcast's message text.
type PendingBroadcastRecipient struct {
	RecipientID int64
	BroadcastID int64
	UserID      int64
	Attempts    int
	Message     string
}
