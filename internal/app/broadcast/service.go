// Package broadcast implements admin-triggered system message campaigns:
// sending a message from the official system account (domain.OfficialSystemUserID,
// 777000) to every user or a hand-picked list. Delivery is a durable outbox
// (store.BroadcastStore's recipient rows) drained by a periodic Worker,
// mirroring internal/app/verification's notification outbox -- the admin
// action only snapshots the recipient list and returns, never sending
// potentially thousands of messages inline within one HTTP request.
package broadcast

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

// messageSender is the narrow port this package needs from
// store.MessageStore: sending a message with an arbitrary SenderUserID, the
// way internal/app/bots's sendServiceBotReplyResult calls it directly at the
// store layer rather than through the auth-checked app.messages.Service
// wrapper (which requires SenderUserID == the authenticated caller).
type messageSender interface {
	SendPrivateText(ctx context.Context, req domain.SendPrivateTextRequest) (domain.SendPrivateTextResult, error)
}

// Service creates broadcasts and drains their delivery outbox.
type Service struct {
	store    store.BroadcastStore
	messages messageSender
	log      *zap.Logger
}

// Option adjusts an optional Service dependency.
type Option func(*Service)

// NewService builds the broadcast service.
func NewService(st store.BroadcastStore, opts ...Option) *Service {
	s := &Service{store: st, log: zap.NewNop()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithMessageSender injects the store used to actually deliver a message.
func WithMessageSender(m messageSender) Option {
	return func(s *Service) {
		if m != nil {
			s.messages = m
		}
	}
}

// WithLogger injects a logger (default zap.NewNop()).
func WithLogger(log *zap.Logger) Option {
	return func(s *Service) {
		if log != nil {
			s.log = log
		}
	}
}

// Ready reports whether both the store and the sender are wired.
func (s *Service) Ready() bool { return s != nil && s.store != nil && s.messages != nil }

// Create validates and snapshots a new broadcast's recipient set, then
// returns immediately: delivery happens asynchronously via RunSendCycle, so
// this never blocks an admin HTTP request on however many recipients there
// are.
func (s *Service) Create(ctx context.Context, message string, targetMode domain.BroadcastTargetMode, recipientUserIDs []int64, createdBy string) (domain.Broadcast, error) {
	if s == nil || s.store == nil {
		return domain.Broadcast{}, fmt.Errorf("broadcast store is not configured")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return domain.Broadcast{}, domain.ErrBroadcastMessageEmpty
	}
	if targetMode != domain.BroadcastTargetAll && targetMode != domain.BroadcastTargetSelected {
		return domain.Broadcast{}, domain.ErrBroadcastInvalid
	}
	return s.store.CreateBroadcast(ctx, message, targetMode, recipientUserIDs, createdBy)
}

// List pages broadcasts newest-first.
func (s *Service) List(ctx context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, nil
	}
	return s.store.ListBroadcasts(ctx, beforeID, limit)
}

// Get returns one broadcast.
func (s *Service) Get(ctx context.Context, id int64) (domain.Broadcast, bool, error) {
	if s == nil || s.store == nil {
		return domain.Broadcast{}, false, nil
	}
	return s.store.BroadcastByID(ctx, id)
}

// RunSendCycle drains up to limit pending recipient rows, sending each from
// domain.OfficialSystemUserID. One recipient's failure (blocked account,
// deleted account, transient error) never blocks the rest of the batch.
func (s *Service) RunSendCycle(ctx context.Context, limit int) (sent int, err error) {
	if s == nil || !s.Ready() {
		return 0, nil
	}
	pending, err := s.store.PendingBroadcastRecipients(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, recipient := range pending {
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		_, sendErr := s.messages.SendPrivateText(ctx, domain.SendPrivateTextRequest{
			SenderUserID:    domain.OfficialSystemUserID,
			RecipientUserID: recipient.UserID,
			// A stable id derived from (broadcast, recipient) makes reprocessing
			// this exact row idempotent at the store layer's random_id dedup,
			// instead of risking a duplicate message if this worker crashes
			// between sending and marking the row delivered.
			RandomID: stableBroadcastRandomID(recipient.BroadcastID, recipient.UserID),
			Message:  recipient.Message,
		})
		if sendErr != nil {
			if markErr := s.store.MarkBroadcastRecipientFailed(ctx, recipient.RecipientID, sendErr.Error()); markErr != nil {
				s.log.Warn("mark broadcast recipient failed",
					zap.Int64("recipient_id", recipient.RecipientID), zap.Error(markErr))
			}
			continue
		}
		if markErr := s.store.MarkBroadcastRecipientSent(ctx, recipient.RecipientID); markErr != nil {
			s.log.Warn("mark broadcast recipient sent",
				zap.Int64("recipient_id", recipient.RecipientID), zap.Error(markErr))
			continue
		}
		sent++
	}
	return sent, nil
}

// stableBroadcastRandomID derives a random_id from (broadcastID, userID) so
// re-processing the same recipient row (after a crash, before it was marked
// delivered) resolves to the same send instead of a duplicate message.
func stableBroadcastRandomID(broadcastID, userID int64) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strconv.FormatInt(broadcastID, 10) + ":" + strconv.FormatInt(userID, 10)))
	v := int64(h.Sum64())
	if v == 0 {
		v = 1
	}
	return v
}
