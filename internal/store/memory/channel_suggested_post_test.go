package memory

import (
	"context"
	"errors"
	"testing"

	"telesrv/internal/domain"
)

func newSuggestedPostMemoryFixture(t *testing.T) (*ChannelStore, domain.Channel, domain.Channel, domain.Peer) {
	t.Helper()
	ctx := context.Background()
	store := NewChannelStore()
	created, err := store.CreateChannel(ctx, domain.CreateChannelRequest{CreatorUserID: 1, Title: "Suggestions", Broadcast: true, Date: 1_700_000_000})
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := store.SetPaidMessagesPrice(ctx, 1, created.Channel.ID, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	mono := store.channels[enabled.Channel.LinkedMonoforumID]
	return store, store.channels[created.Channel.ID], mono, domain.Peer{Type: domain.PeerTypeUser, ID: 42}
}

func TestMonoforumManagerRequiresManageDirectMessages(t *testing.T) {
	ctx := context.Background()
	store, parent, mono, subscriber := newSuggestedPostMemoryFixture(t)
	store.mu.Lock()
	store.members[parent.ID][2] = domain.ChannelMember{ChannelID: parent.ID, UserID: 2, Role: domain.ChannelRoleAdmin, Status: domain.ChannelMemberActive, AdminRights: domain.ChannelAdminRights{PostMessages: true}}
	store.mu.Unlock()
	if _, manager, err := store.ResolveMonoforumSend(ctx, 2, mono.ID); err != nil || manager {
		t.Fatalf("ordinary admin resolved as manager: manager=%v err=%v", manager, err)
	}
	if _, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: 2, SavedPeer: subscriber, RandomID: 1, Message: "must not send", Date: 1_700_000_010}); !errors.Is(err, domain.ErrChannelAdminRequired) {
		t.Fatalf("ordinary admin send err=%v, want admin required", err)
	}
	fromSubscriber, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 10, Message: "private", Date: 1_700_000_010})
	if err != nil {
		t.Fatal(err)
	}
	if containsInt64(fromSubscriber.Recipients, 2) {
		t.Fatalf("ordinary admin leaked into recipients: %v", fromSubscriber.Recipients)
	}
	dialogs, err := store.ListChannelDialogs(ctx, 2, domain.DialogFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, dialog := range dialogs.Dialogs {
		if dialog.Peer.ID == mono.ID {
			t.Fatalf("ordinary admin received monoforum dialog")
		}
	}
	store.mu.Lock()
	member := store.members[parent.ID][2]
	member.AdminRights.ManageDirectMessages = true
	store.members[parent.ID][2] = member
	store.mu.Unlock()
	if _, manager, err := store.ResolveMonoforumSend(ctx, 2, mono.ID); err != nil || !manager {
		t.Fatalf("DM manager not resolved: manager=%v err=%v", manager, err)
	}
	dialogs, err = store.ListChannelDialogs(ctx, 2, domain.DialogFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	foundMono := false
	for _, dialog := range dialogs.Dialogs {
		foundMono = foundMono || dialog.Peer.ID == mono.ID
	}
	if !foundMono {
		t.Fatalf("DM manager missing monoforum dialog")
	}
	if _, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: 2, SavedPeer: subscriber, RandomID: 2, Message: "allowed", Date: 1_700_000_011}); err != nil {
		t.Fatalf("DM manager send: %v", err)
	}
}

func TestSuggestedPostApprovalRefundAndSettlement(t *testing.T) {
	ctx := context.Background()
	store, parent, mono, subscriber := newSuggestedPostMemoryFixture(t)

	suggestion, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 11, Message: "publish me", SuggestedPost: &domain.SuggestedPost{Price: &domain.SuggestedPostPrice{Kind: domain.SuggestedPostPriceStars, Amount: 10}}, Date: 1_700_000_100})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: suggestion.Message.ID, Date: 1_700_000_200})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != domain.SuggestedPostStateCompleted || approved.OriginalEvent.Type != domain.ChannelUpdateEditMessage || approved.ServiceMessage.Action == nil || approved.ServiceMessage.Action.Type != domain.ChannelActionSuggestedPostApproval || approved.Published == nil {
		t.Fatalf("approval result=%+v", approved)
	}
	if approved.OriginalMessage.SuggestedPost.ScheduleDate != 1_700_000_200 || approved.ServiceMessage.Action.SuggestedPostScheduleDate != 1_700_000_200 {
		t.Fatalf("immediate approval dates original/action=%d/%d, want commit date", approved.OriginalMessage.SuggestedPost.ScheduleDate, approved.ServiceMessage.Action.SuggestedPostScheduleDate)
	}
	duplicate, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: suggestion.Message.ID, Date: 1_700_000_201})
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	if duplicate.OriginalMessage.SuggestedPost.ScheduleDate != 1_700_000_200 || duplicate.ServiceMessage.Action.SuggestedPostScheduleDate != 1_700_000_200 {
		t.Fatalf("duplicate changed immediate approval date: %+v", duplicate)
	}
	// An immediate approval is already terminal (Completed): there is nothing
	// left to settle, so deleting the published post afterwards must not
	// surface it again through the lifecycle worker.
	store.mu.Lock()
	for i := range store.messages[parent.ID] {
		if store.messages[parent.ID][i].ID == approved.Published.Message.ID {
			store.messages[parent.ID][i].Deleted = true
		}
	}
	store.mu.Unlock()
	if lifecycle, err := store.ProcessSuggestedPostLifecycle(ctx, domain.SuggestedPostLifecycleRequest{Now: 1_700_000_300, Limit: 10}); err != nil || len(lifecycle) != 0 {
		t.Fatalf("already-completed post must not be revisited: lifecycle=%+v err=%v", lifecycle, err)
	}

	// A scheduled (not-yet-due) approval still refunds via the lifecycle
	// worker if the suggestion is deleted before its publish date.
	second, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 12, Message: "cancel scheduled", SuggestedPost: &domain.SuggestedPost{Price: &domain.SuggestedPostPrice{Kind: domain.SuggestedPostPriceStars, Amount: 20}}, Date: 1_700_000_400})
	if err != nil {
		t.Fatal(err)
	}
	scheduled, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: second.Message.ID, ScheduleDate: 1_700_000_900, Date: 1_700_000_401})
	if err != nil || scheduled.State != domain.SuggestedPostStateScheduled {
		t.Fatalf("scheduled approval=%+v err=%v", scheduled, err)
	}
	store.mu.Lock()
	for i := range store.messages[mono.ID] {
		if store.messages[mono.ID][i].ID == second.Message.ID {
			store.messages[mono.ID][i].Deleted = true
		}
	}
	store.mu.Unlock()
	lifecycle, err := store.ProcessSuggestedPostLifecycle(ctx, domain.SuggestedPostLifecycleRequest{Now: 1_700_000_500, Limit: 10})
	if err != nil || len(lifecycle) != 1 || lifecycle[0].State != domain.SuggestedPostStateRefunded {
		t.Fatalf("refund lifecycle=%+v err=%v", lifecycle, err)
	}

	third, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 13, Message: "settle me", SuggestedPost: &domain.SuggestedPost{Price: &domain.SuggestedPostPrice{Kind: domain.SuggestedPostPriceStars, Amount: 20}}, Date: 1_700_000_600})
	if err != nil {
		t.Fatal(err)
	}
	settling, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: third.Message.ID, Date: 1_700_000_700})
	if err != nil || settling.State != domain.SuggestedPostStateCompleted {
		t.Fatalf("third approval=%+v err=%v", settling, err)
	}
}

func TestSuggestedPostScheduleRetryAndRoleMatrix(t *testing.T) {
	ctx := context.Background()
	store, parent, mono, subscriber := newSuggestedPostMemoryFixture(t)
	suggestion, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 21, Message: "later", SuggestedPost: &domain.SuggestedPost{Price: &domain.SuggestedPostPrice{Kind: domain.SuggestedPostPriceStars, Amount: 10}}, Date: 1_700_001_000})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: suggestion.Message.ID, ScheduleDate: 1_700_001_400, Date: 1_700_001_050})
	if err != nil || accepted.State != domain.SuggestedPostStateScheduled || accepted.Published != nil {
		t.Fatalf("scheduled=%+v err=%v", accepted, err)
	}
	again, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: suggestion.Message.ID, ScheduleDate: 1_700_001_400, Date: 1_700_001_051})
	if err != nil || !again.Duplicate {
		t.Fatalf("schedule retry=%+v err=%v", again, err)
	}
	due, err := store.ProcessSuggestedPostLifecycle(ctx, domain.SuggestedPostLifecycleRequest{Now: 1_700_001_400, Limit: 10})
	if err != nil || len(due) != 1 || due[0].Published == nil || due[0].State != domain.SuggestedPostStateCompleted {
		t.Fatalf("due=%+v err=%v", due, err)
	}

	store.mu.Lock()
	store.members[parent.ID][2] = domain.ChannelMember{ChannelID: parent.ID, UserID: 2, Role: domain.ChannelRoleAdmin, Status: domain.ChannelMemberActive, AdminRights: domain.ChannelAdminRights{ManageDirectMessages: true}}
	store.mu.Unlock()
	third, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 22, Message: "decline only", SuggestedPost: &domain.SuggestedPost{}, Date: 1_700_002_000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 2, MonoforumID: mono.ID, MessageID: third.Message.ID, Date: 1_700_002_100}); !errors.Is(err, domain.ErrSuggestedPostApprovalForbidden) {
		t.Fatalf("manager without post right approve err=%v", err)
	}
	rejected, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 2, MonoforumID: mono.ID, MessageID: third.Message.ID, Reject: true, RejectComment: "no", Date: 1_700_002_100})
	if err != nil || rejected.State != domain.SuggestedPostStateRejected {
		t.Fatalf("decline=%+v err=%v", rejected, err)
	}
}

func TestSuggestedPostApprovalAcceptsDelayedScheduleAndKeepsPTSIdempotent(t *testing.T) {
	ctx := context.Background()
	store, parent, mono, subscriber := newSuggestedPostMemoryFixture(t)

	const now = 1_700_010_000
	near, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{
		MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber,
		RandomID: 71, Message: "near schedule", SuggestedPost: &domain.SuggestedPost{}, Date: now - 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	nearDate := now + 2*60
	accepted, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{
		UserID: 1, MonoforumID: mono.ID, MessageID: near.Message.ID,
		ScheduleDate: nearDate, Date: now,
	})
	if err != nil {
		t.Fatalf("accept schedule below former five-minute gate: %v", err)
	}
	if accepted.State != domain.SuggestedPostStateScheduled || accepted.Published != nil ||
		accepted.OriginalMessage.SuggestedPost.ScheduleDate != nearDate ||
		accepted.ServiceMessage.Action == nil ||
		accepted.ServiceMessage.Action.SuggestedPostScheduleDate != nearDate {
		t.Fatalf("near schedule approval = %+v, want scheduled at %d", accepted, nearDate)
	}
	monoPts, parentPts := store.channels[mono.ID].Pts, store.channels[parent.ID].Pts
	monoEvents, parentEvents := len(store.events[mono.ID]), len(store.events[parent.ID])
	replay, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{
		UserID: 1, MonoforumID: mono.ID, MessageID: near.Message.ID,
		ScheduleDate: nearDate, Date: now + 10*60,
	})
	if err != nil || !replay.Duplicate {
		t.Fatalf("late duplicate approval = %+v err=%v", replay, err)
	}
	if store.channels[mono.ID].Pts != monoPts || store.channels[parent.ID].Pts != parentPts ||
		len(store.events[mono.ID]) != monoEvents || len(store.events[parent.ID]) != parentEvents {
		t.Fatal("late duplicate approval advanced PTS or appended an event")
	}

	due, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{
		MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber,
		RandomID: 72, Message: "already due", SuggestedPost: &domain.SuggestedPost{}, Date: now + 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	approvedAt := now + 30
	dueResult, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{
		UserID: 1, MonoforumID: mono.ID, MessageID: due.Message.ID,
		ScheduleDate: now - 1, Date: approvedAt,
	})
	if err != nil {
		t.Fatalf("approve already-due schedule: %v", err)
	}
	if dueResult.State != domain.SuggestedPostStateCompleted || dueResult.Published == nil ||
		dueResult.OriginalMessage.SuggestedPost.ScheduleDate != approvedAt ||
		dueResult.ServiceMessage.Action == nil ||
		dueResult.ServiceMessage.Action.SuggestedPostScheduleDate != approvedAt {
		t.Fatalf("due schedule approval = %+v, want immediate publish at %d", dueResult, approvedAt)
	}

	far, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{
		MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber,
		RandomID: 73, Message: "too far", SuggestedPost: &domain.SuggestedPost{}, Date: now + 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	monoPts, parentPts = store.channels[mono.ID].Pts, store.channels[parent.ID].Pts
	monoEvents, parentEvents = len(store.events[mono.ID]), len(store.events[parent.ID])
	if _, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{
		UserID: 1, MonoforumID: mono.ID, MessageID: far.Message.ID,
		ScheduleDate: approvedAt + domain.MaxSuggestedPostScheduleDelay + 1, Date: approvedAt,
	}); !errors.Is(err, domain.ErrSuggestedPostInvalid) {
		t.Fatalf("far schedule err=%v, want suggested post invalid", err)
	}
	if store.channels[mono.ID].Pts != monoPts || store.channels[parent.ID].Pts != parentPts ||
		len(store.events[mono.ID]) != monoEvents || len(store.events[parent.ID]) != parentEvents {
		t.Fatal("far schedule rejection advanced PTS or appended an event")
	}
}

func TestChannelAuthoredSuggestedPostAcceptedBySubscriber(t *testing.T) {
	ctx := context.Background()
	store, _, mono, subscriber := newSuggestedPostMemoryFixture(t)
	fromChannel, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: 1, SavedPeer: subscriber, RandomID: 31, Message: "channel proposal", SuggestedPost: &domain.SuggestedPost{}, Date: 1_700_003_000})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: subscriber.ID, MonoforumID: mono.ID, MessageID: fromChannel.Message.ID, Date: 1_700_003_100})
	if err != nil || result.State != domain.SuggestedPostStateCompleted || result.Published == nil {
		t.Fatalf("subscriber approval=%+v err=%v", result, err)
	}
}

func TestScheduledSuggestedPostDeletionRefundsBeforePublication(t *testing.T) {
	ctx := context.Background()
	store, _, mono, subscriber := newSuggestedPostMemoryFixture(t)
	suggestion, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber, RandomID: 41, Message: "cancel scheduled", SuggestedPost: &domain.SuggestedPost{Price: &domain.SuggestedPostPrice{Kind: domain.SuggestedPostPriceStars, Amount: 10}}, Date: 1_700_004_000})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{UserID: 1, MonoforumID: mono.ID, MessageID: suggestion.Message.ID, ScheduleDate: 1_700_004_600, Date: 1_700_004_000})
	if err != nil || accepted.State != domain.SuggestedPostStateScheduled {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	store.mu.Lock()
	for i := range store.messages[mono.ID] {
		if store.messages[mono.ID][i].ID == suggestion.Message.ID {
			store.messages[mono.ID][i].Deleted = true
		}
	}
	store.mu.Unlock()
	resolved, err := store.ProcessSuggestedPostLifecycle(ctx, domain.SuggestedPostLifecycleRequest{Now: 1_700_004_100, Limit: 10})
	if err != nil || len(resolved) != 1 || resolved[0].State != domain.SuggestedPostStateRefunded || resolved[0].Published != nil {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
}

func TestSuggestedPostLifecycleFailsFastOnCorruptAcceptedState(t *testing.T) {
	ctx := context.Background()
	store, _, mono, subscriber := newSuggestedPostMemoryFixture(t)
	suggestion, err := store.SendMonoforumMessage(ctx, domain.SendMonoforumMessageRequest{
		MonoforumID: mono.ID, SenderUserID: subscriber.ID, SavedPeer: subscriber,
		RandomID: 61, Message: "must fail fast", SuggestedPost: &domain.SuggestedPost{}, Date: 1_700_006_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ToggleSuggestedPostApproval(ctx, domain.ToggleSuggestedPostApprovalRequest{
		UserID: 1, MonoforumID: mono.ID, MessageID: suggestion.Message.ID,
		ScheduleDate: 1_700_006_600, Date: 1_700_006_000,
	}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	for i, message := range store.messages[mono.ID] {
		if message.ID == suggestion.Message.ID {
			store.messages[mono.ID] = append(store.messages[mono.ID][:i], store.messages[mono.ID][i+1:]...)
			break
		}
	}
	store.mu.Unlock()
	if _, err := store.ProcessSuggestedPostLifecycle(ctx, domain.SuggestedPostLifecycleRequest{Now: 1_700_006_100, Limit: 10}); err == nil {
		t.Fatal("corrupt accepted suggestion was silently skipped")
	}
}

// An immediately-approved suggested post is terminal (Completed) the moment
// it is published -- there is no settlement window anymore (telesrv has no
// Stars economy, so nothing is ever collected that would need settling).
// Deleting the published post afterwards is therefore a no-op for the
// suggested-post lifecycle; see TestSuggestedPostApprovalRefundAndSettlement
// for that assertion.
