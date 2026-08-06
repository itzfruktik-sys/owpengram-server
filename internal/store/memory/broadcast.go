package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

// BroadcastStore is the in-memory implementation of store.BroadcastStore,
// used by admin/app unit tests.
type BroadcastStore struct {
	mu         sync.Mutex
	broadcasts map[int64]domain.Broadcast
	recipients map[int64]*memBroadcastRecipient
	nextBID    int64
	nextRID    int64
}

type memBroadcastRecipient struct {
	domain.BroadcastRecipient
	message string
}

func NewBroadcastStore() *BroadcastStore {
	return &BroadcastStore{
		broadcasts: make(map[int64]domain.Broadcast),
		recipients: make(map[int64]*memBroadcastRecipient),
	}
}

var _ store.BroadcastStore = (*BroadcastStore)(nil)

func (s *BroadcastStore) CreateBroadcast(_ context.Context, message string, targetMode domain.BroadcastTargetMode, recipientUserIDs []int64, createdBy string) (domain.Broadcast, error) {
	if len(recipientUserIDs) == 0 {
		return domain.Broadcast{}, domain.ErrBroadcastNoRecipients
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextBID++
	b := domain.Broadcast{
		ID:         s.nextBID,
		Message:    message,
		TargetMode: targetMode,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC(),
	}
	seen := make(map[int64]bool, len(recipientUserIDs))
	for _, userID := range recipientUserIDs {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		s.nextRID++
		s.recipients[s.nextRID] = &memBroadcastRecipient{
			BroadcastRecipient: domain.BroadcastRecipient{
				ID:          s.nextRID,
				BroadcastID: b.ID,
				UserID:      userID,
				Status:      domain.BroadcastRecipientPending,
			},
			message: message,
		}
		b.TotalCount++
	}
	s.broadcasts[b.ID] = b
	return b, nil
}

func (s *BroadcastStore) PendingBroadcastRecipients(_ context.Context, limit int) ([]store.PendingBroadcastRecipient, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Iteration order over a map is unspecified; sort by recipient id (assigned
	// in creation order) so this matches the postgres backend's "oldest first".
	ids := make([]int64, 0, len(s.recipients))
	for id, r := range s.recipients {
		if r.Status == domain.BroadcastRecipientPending {
			ids = append(ids, id)
		}
	}
	sortInt64s(ids)
	out := make([]store.PendingBroadcastRecipient, 0, limit)
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		r := s.recipients[id]
		out = append(out, store.PendingBroadcastRecipient{
			RecipientID: r.ID, BroadcastID: r.BroadcastID, UserID: r.UserID, Attempts: r.Attempts, Message: r.message,
		})
	}
	return out, nil
}

func (s *BroadcastStore) MarkBroadcastRecipientSent(_ context.Context, recipientID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recipients[recipientID]
	if !ok || r.Status != domain.BroadcastRecipientPending {
		return nil
	}
	r.Status = domain.BroadcastRecipientSent
	now := time.Now().UTC()
	r.SentAt = &now
	r.LastError = ""
	return nil
}

func (s *BroadcastStore) MarkBroadcastRecipientFailed(_ context.Context, recipientID int64, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recipients[recipientID]
	if !ok || r.Status != domain.BroadcastRecipientPending {
		return nil
	}
	r.Attempts++
	r.LastError = reason
	if r.Attempts >= domain.MaxBroadcastRecipientAttempts {
		r.Status = domain.BroadcastRecipientFailed
	}
	return nil
}

func (s *BroadcastStore) countsFor(broadcastID int64) (sent, failed int) {
	for _, r := range s.recipients {
		if r.BroadcastID != broadcastID {
			continue
		}
		switch r.Status {
		case domain.BroadcastRecipientSent:
			sent++
		case domain.BroadcastRecipientFailed:
			failed++
		}
	}
	return sent, failed
}

func (s *BroadcastStore) ListBroadcasts(_ context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.broadcasts))
	for id := range s.broadcasts {
		if beforeID == 0 || id < beforeID {
			ids = append(ids, id)
		}
	}
	sortInt64sDesc(ids)
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	out := make([]domain.Broadcast, 0, len(ids))
	for _, id := range ids {
		b := s.broadcasts[id]
		b.SentCount, b.FailedCount = s.countsFor(id)
		out = append(out, b)
	}
	return out, hasMore, nil
}

func (s *BroadcastStore) BroadcastByID(_ context.Context, id int64) (domain.Broadcast, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.broadcasts[id]
	if !ok {
		return domain.Broadcast{}, false, nil
	}
	b.SentCount, b.FailedCount = s.countsFor(id)
	return b, true, nil
}

func sortInt64s(v []int64) {
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
}

func sortInt64sDesc(v []int64) {
	sort.Slice(v, func(i, j int) bool { return v[i] > v[j] })
}
