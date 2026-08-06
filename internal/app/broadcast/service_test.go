package broadcast

import (
	"context"
	"errors"
	"testing"

	"telesrv/internal/domain"
	"telesrv/internal/store/memory"
)

type fakeSender struct {
	sent    []domain.SendPrivateTextRequest
	failFor map[int64]bool // fail every send to this recipient user id
}

func (f *fakeSender) SendPrivateText(_ context.Context, req domain.SendPrivateTextRequest) (domain.SendPrivateTextResult, error) {
	if f.failFor[req.RecipientUserID] {
		return domain.SendPrivateTextResult{}, errors.New("simulated send failure")
	}
	f.sent = append(f.sent, req)
	return domain.SendPrivateTextResult{}, nil
}

func TestCreateValidatesInput(t *testing.T) {
	svc := NewService(memory.NewBroadcastStore(), WithMessageSender(&fakeSender{}))
	ctx := context.Background()

	if _, err := svc.Create(ctx, "  ", domain.BroadcastTargetAll, []int64{1}, "admin"); !errors.Is(err, domain.ErrBroadcastMessageEmpty) {
		t.Fatalf("empty message: err = %v, want ErrBroadcastMessageEmpty", err)
	}
	if _, err := svc.Create(ctx, "hi", domain.BroadcastTargetMode("bogus"), []int64{1}, "admin"); !errors.Is(err, domain.ErrBroadcastInvalid) {
		t.Fatalf("bad target mode: err = %v, want ErrBroadcastInvalid", err)
	}
	if _, err := svc.Create(ctx, "hi", domain.BroadcastTargetSelected, nil, "admin"); !errors.Is(err, domain.ErrBroadcastNoRecipients) {
		t.Fatalf("no recipients: err = %v, want ErrBroadcastNoRecipients", err)
	}

	created, err := svc.Create(ctx, "  News! ", domain.BroadcastTargetSelected, []int64{10, 20, 20}, "admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Message != "News!" {
		t.Fatalf("Message = %q, want trimmed %q", created.Message, "News!")
	}
	// The duplicate recipient (20 twice) collapses to one row.
	if created.TotalCount != 2 {
		t.Fatalf("TotalCount = %d, want 2 (duplicate recipient collapsed)", created.TotalCount)
	}
}

func TestRunSendCycleDeliversAndCounts(t *testing.T) {
	store := memory.NewBroadcastStore()
	sender := &fakeSender{}
	svc := NewService(store, WithMessageSender(sender))
	ctx := context.Background()

	created, err := svc.Create(ctx, "Update available", domain.BroadcastTargetSelected, []int64{101, 102, 103}, "admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sent, err := svc.RunSendCycle(ctx, 10)
	if err != nil {
		t.Fatalf("RunSendCycle: %v", err)
	}
	if sent != 3 {
		t.Fatalf("sent = %d, want 3", sent)
	}
	if len(sender.sent) != 3 {
		t.Fatalf("sender received %d sends, want 3", len(sender.sent))
	}
	for _, req := range sender.sent {
		if req.SenderUserID != domain.OfficialSystemUserID {
			t.Fatalf("SenderUserID = %d, want OfficialSystemUserID (%d)", req.SenderUserID, domain.OfficialSystemUserID)
		}
		if req.Message != "Update available" {
			t.Fatalf("Message = %q, want %q", req.Message, "Update available")
		}
	}

	got, found, err := svc.Get(ctx, created.ID)
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if got.SentCount != 3 || got.FailedCount != 0 {
		t.Fatalf("counts = sent:%d failed:%d, want sent:3 failed:0", got.SentCount, got.FailedCount)
	}

	// A second cycle finds nothing left pending.
	sent, err = svc.RunSendCycle(ctx, 10)
	if err != nil {
		t.Fatalf("RunSendCycle (second): %v", err)
	}
	if sent != 0 {
		t.Fatalf("second cycle sent = %d, want 0 (nothing pending)", sent)
	}
}

func TestRunSendCycleRetriesThenTerminatesFailures(t *testing.T) {
	store := memory.NewBroadcastStore()
	sender := &fakeSender{failFor: map[int64]bool{999: true}}
	svc := NewService(store, WithMessageSender(sender))
	ctx := context.Background()

	if _, err := svc.Create(ctx, "will fail", domain.BroadcastTargetSelected, []int64{999}, "admin"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Run one cycle per attempt, up to the cap; the row must stay pending
	// (retried) below the cap and become terminal at it.
	for i := 0; i < domain.MaxBroadcastRecipientAttempts; i++ {
		sent, err := svc.RunSendCycle(ctx, 10)
		if err != nil {
			t.Fatalf("RunSendCycle attempt %d: %v", i+1, err)
		}
		if sent != 0 {
			t.Fatalf("attempt %d: sent = %d, want 0 (always fails)", i+1, sent)
		}
	}

	// One more cycle: the row is now terminal ('failed'), so PendingBroadcastRecipients
	// must not return it, and RunSendCycle finds nothing left to attempt.
	pending, err := store.PendingBroadcastRecipients(ctx, 10)
	if err != nil {
		t.Fatalf("PendingBroadcastRecipients: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %+v, want empty (recipient should be terminally failed)", pending)
	}
}
