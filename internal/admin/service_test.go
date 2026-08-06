package admin

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	usernamesapp "telesrv/internal/app/usernames"
	"telesrv/internal/domain"
)

// Compile-time proof that the shipped use-case services satisfy the admin ports.
// cmd/telesrv wires *usernames.Service into Dependencies.Usernames directly,
// so a drifting method set has to fail here rather than at integration time.
var (
	_ CollectibleUsernamesService = (*usernamesapp.Service)(nil)
)

func TestSetAccountFrozenDryRunExecuteAndIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	restrictions := &fakeRestrictionStore{}
	notifier := &fakeAccountFreezeNotifier{}
	svc := NewService(Dependencies{
		Commands:       repo,
		Restrictions:   restrictions,
		FreezeNotifier: notifier,
		Now:            fixedNow,
	})

	dry, err := svc.SetAccountFrozen(ctx, SetAccountFrozenRequest{
		CommandMeta: CommandMeta{CommandID: "dry-freeze", Actor: "ops", Reason: "test", DryRun: true},
		UserID:      1001,
		Frozen:      true,
		Until:       fixedNow().Add(7 * 24 * time.Hour),
		AppealURL:   "https://appeals.example.test/account/1001",
	})
	if err != nil {
		t.Fatalf("dry-run freeze: %v", err)
	}
	if !dry.DryRun || dry.Status != string(domain.AdminCommandCompleted) || restrictions.setCalls != 0 {
		t.Fatalf("dry-run result=%+v setCalls=%d, want completed dry-run without mutation", dry, restrictions.setCalls)
	}

	execReq := SetAccountFrozenRequest{
		CommandMeta: CommandMeta{CommandID: "exec-freeze", Actor: "ops", Reason: "incident", DryRun: false},
		UserID:      1001,
		Frozen:      true,
		Until:       fixedNow().Add(7 * 24 * time.Hour),
		AppealURL:   "https://appeals.example.test/account/1001",
	}
	exec, err := svc.SetAccountFrozen(ctx, execReq)
	if err != nil {
		t.Fatalf("execute freeze: %v", err)
	}
	if exec.Status != string(domain.AdminCommandCompleted) || restrictions.setCalls != 1 {
		t.Fatalf("execute result=%+v setCalls=%d", exec, restrictions.setCalls)
	}
	if len(notifier.items) != 1 || notifier.items[0].UserID != 1001 || !notifier.items[0].Frozen || notifier.items[0].Version != 1 {
		t.Fatalf("freeze notifications = %+v, want one versioned frozen state", notifier.items)
	}
	if err := svc.CanSendMessages(ctx, 1001); !errors.Is(err, domain.ErrUserFrozen) {
		t.Fatalf("CanSendMessages err=%v, want ErrUserFrozen", err)
	}
	freeze, found, err := svc.AccountFreeze(ctx, 1001)
	if err != nil || !found || !freeze.Frozen || !freeze.Since.Equal(fixedNow()) || freeze.AppealURL != execReq.AppealURL {
		t.Fatalf("AccountFreeze = %+v found=%v err=%v", freeze, found, err)
	}

	again, err := svc.SetAccountFrozen(ctx, execReq)
	if err != nil {
		t.Fatalf("duplicate freeze: %v", err)
	}
	if !again.AlreadyExecuted || restrictions.setCalls != 1 {
		t.Fatalf("duplicate result=%+v setCalls=%d, want idempotent replay", again, restrictions.setCalls)
	}
	if len(notifier.items) != 1 {
		t.Fatalf("idempotent replay emitted duplicate notification: %+v", notifier.items)
	}
}

func TestCreateBotReturnsTokenOnceWithoutPersistingCredential(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	bots := &fakeBotService{token: "test-one-time-bot-credential"}
	svc := NewService(Dependencies{Commands: repo, Bots: bots, Now: fixedNow})
	req := CreateBotRequest{
		CommandMeta: CommandMeta{CommandID: "create-bot-once", Actor: "ops", Reason: "requested"},
		OwnerUserID: 1001,
		Name:        "Audit Safe Bot",
		Username:    "audit_safe_bot",
	}

	first, err := svc.CreateBot(ctx, req)
	if err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if first.Details["token"] != bots.token || bots.createCalls != 1 {
		t.Fatalf("first result=%+v createCalls=%d", first, bots.createCalls)
	}
	stored := repo.items[req.CommandID].ResultJSON
	if bytes.Contains(stored, []byte(bots.token)) || bytes.Contains(stored, []byte(`"token"`)) {
		t.Fatalf("persisted admin result contains bot credential: %s", stored)
	}

	replay, err := svc.CreateBot(ctx, req)
	if err != nil {
		t.Fatalf("CreateBot replay: %v", err)
	}
	if !replay.AlreadyExecuted || bots.createCalls != 1 {
		t.Fatalf("replay=%+v createCalls=%d", replay, bots.createCalls)
	}
	if _, leaked := replay.Details["token"]; leaked {
		t.Fatalf("replayed command exposed one-time bot token: %+v", replay)
	}
}

func TestModerationFlagsRejectImpossibleScamFakeState(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	users := &fakeUsersService{users: map[int64]domain.User{1001: {ID: 1001}}}
	channels := &fakeChannelsService{channels: map[int64]domain.Channel{2001: {
		ID: 2001, Megagroup: true,
	}}}
	svc := NewService(Dependencies{Commands: repo, Users: users, Channels: channels, Now: fixedNow})
	meta := CommandMeta{CommandID: "invalid-user-flags", Actor: "ops", Reason: "test"}
	if _, err := svc.SetUserFlags(ctx, SetUserFlagsRequest{
		CommandMeta: meta, UserID: 1001, Scam: true, Fake: true,
	}); !errors.Is(err, domain.ErrPeerModerationFlagsInvalid) {
		t.Fatalf("SetUserFlags error=%v", err)
	}
	meta.CommandID = "invalid-channel-flags"
	if _, err := svc.SetChannelFlags(ctx, SetChannelFlagsRequest{
		CommandMeta: meta, ChannelID: 2001, Scam: true, Fake: true,
	}); !errors.Is(err, domain.ErrPeerModerationFlagsInvalid) {
		t.Fatalf("SetChannelFlags error=%v", err)
	}
	if len(repo.items) != 0 || users.users[1001].Scam || users.users[1001].Fake ||
		channels.channels[2001].Scam || channels.channels[2001].Fake {
		t.Fatalf("invalid moderation state reached command/store boundary: commands=%d user=%+v channel=%+v",
			len(repo.items), users.users[1001], channels.channels[2001])
	}
}

func TestSetUserFlagsUsesNonPTSModerationNotifier(t *testing.T) {
	ctx := context.Background()
	users := &fakeUsersService{users: map[int64]domain.User{
		1001: {ID: 1001, FirstName: "Alice"},
	}}
	ordinaryNotifier := &fakeUserNotifier{}
	moderationNotifier := &fakeUserModerationNotifier{}
	svc := NewService(Dependencies{
		Commands:               newMemoryCommandRepo(),
		Users:                  users,
		UserNotifier:           ordinaryNotifier,
		UserModerationNotifier: moderationNotifier,
		Now:                    fixedNow,
	})

	if _, err := svc.SetUserFlags(ctx, SetUserFlagsRequest{
		CommandMeta: CommandMeta{CommandID: "set-user-scam", Actor: "ops", Reason: "confirmed report"},
		UserID:      1001,
		Scam:        true,
	}); err != nil {
		t.Fatalf("SetUserFlags: %v", err)
	}
	if got := users.users[1001]; !got.Scam || got.Fake {
		t.Fatalf("updated user = %+v", got)
	}
	if len(moderationNotifier.users) != 1 || moderationNotifier.users[0] != 1001 {
		t.Fatalf("moderation notifications = %v", moderationNotifier.users)
	}
	if len(ordinaryNotifier.users) != 0 {
		t.Fatalf("ordinary notifications = %v, want dedicated non-PTS path", ordinaryNotifier.users)
	}
}

func TestAccountFreezesBatchesAndReturnsOnlyActiveFacts(t *testing.T) {
	now := fixedNow()
	store := &fakeBatchRestrictionStore{fakeRestrictionStore: fakeRestrictionStore{items: map[int64]domain.AccountFreeze{
		1001: {
			UserID: 1001, Frozen: true, Version: 2, Since: now,
			Until: now.Add(time.Hour), AppealURL: "https://appeals.example.test/1001",
		},
		1002: {UserID: 1002, Frozen: false, Version: 4},
	}}}
	svc := NewService(Dependencies{Restrictions: store, Now: fixedNow})

	got, err := svc.AccountFreezes(context.Background(), []int64{1001, 1001, 0, 1002})
	if err != nil {
		t.Fatalf("AccountFreezes: %v", err)
	}
	if len(store.requests) != 1 || !reflect.DeepEqual(store.requests[0], []int64{1001, 1002}) {
		t.Fatalf("batch requests = %v, want one deduplicated request", store.requests)
	}
	if len(got) != 1 || !got[1001].Frozen || got[1001].Version != 2 {
		t.Fatalf("AccountFreezes = %+v, want active user 1001 only", got)
	}
}

func TestSetAccountFrozenRejectsIncompleteStateAndUnfreezeClearsOverlay(t *testing.T) {
	ctx := context.Background()
	restrictions := &fakeRestrictionStore{}
	svc := NewService(Dependencies{Commands: newMemoryCommandRepo(), Restrictions: restrictions, Now: fixedNow})
	for _, req := range []SetAccountFrozenRequest{
		{CommandMeta: CommandMeta{CommandID: "bad-until", Actor: "ops", Reason: "test"}, UserID: 1001, Frozen: true, Until: fixedNow(), AppealURL: "https://appeals.example.test"},
		{CommandMeta: CommandMeta{CommandID: "too-far", Actor: "ops", Reason: "test"}, UserID: 1001, Frozen: true, Until: time.Unix(1<<31, 0), AppealURL: "https://appeals.example.test"},
		{CommandMeta: CommandMeta{CommandID: "bad-url", Actor: "ops", Reason: "test"}, UserID: 1001, Frozen: true, Until: fixedNow().Add(time.Hour), AppealURL: "javascript:bad"},
		{CommandMeta: CommandMeta{CommandID: "long-url", Actor: "ops", Reason: "test"}, UserID: 1001, Frozen: true, Until: fixedNow().Add(time.Hour), AppealURL: "https://appeals.example.test/" + strings.Repeat("x", maxFreezeAppealURLLength)},
	} {
		if _, err := svc.SetAccountFrozen(ctx, req); err == nil {
			t.Fatalf("SetAccountFrozen(%+v) succeeded", req)
		}
	}
	freezeReq := SetAccountFrozenRequest{CommandMeta: CommandMeta{CommandID: "freeze", Actor: "ops", Reason: "test"}, UserID: 1001, Frozen: true, Until: fixedNow().Add(24 * time.Hour), AppealURL: "https://appeals.example.test"}
	if _, err := svc.SetAccountFrozen(ctx, freezeReq); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetAccountFrozen(ctx, SetAccountFrozenRequest{CommandMeta: CommandMeta{CommandID: "unfreeze", Actor: "ops", Reason: "accepted"}, UserID: 1001}); err != nil {
		t.Fatal(err)
	}
	freeze, found, err := svc.AccountFreeze(ctx, 1001)
	if err != nil || !found || freeze.Frozen || !freeze.Since.IsZero() || !freeze.Until.IsZero() || freeze.AppealURL != "" {
		t.Fatalf("unfrozen state = %+v found=%v err=%v", freeze, found, err)
	}
}

func TestSetAccountFrozenUpdatePreservesOriginalSince(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	restrictions := &fakeRestrictionStore{}
	svc := NewService(Dependencies{
		Commands:     newMemoryCommandRepo(),
		Restrictions: restrictions,
		Now:          func() time.Time { return now },
	})
	if _, err := svc.SetAccountFrozen(ctx, SetAccountFrozenRequest{
		CommandMeta: CommandMeta{CommandID: "freeze-initial", Actor: "ops", Reason: "review"},
		UserID:      1001,
		Frozen:      true,
		Until:       now.Add(24 * time.Hour),
		AppealURL:   "https://appeals.example.test/initial",
	}); err != nil {
		t.Fatal(err)
	}
	originalSince := now
	now = now.Add(2 * time.Hour)
	updatedUntil := now.Add(72 * time.Hour)
	if _, err := svc.SetAccountFrozen(ctx, SetAccountFrozenRequest{
		CommandMeta: CommandMeta{CommandID: "freeze-update", Actor: "ops", Reason: "extend review"},
		UserID:      1001,
		Frozen:      true,
		Until:       updatedUntil,
		AppealURL:   "https://appeals.example.test/updated",
	}); err != nil {
		t.Fatal(err)
	}
	freeze, found, err := svc.AccountFreeze(ctx, 1001)
	if err != nil || !found || !freeze.Since.Equal(originalSince) || !freeze.Until.Equal(updatedUntil) ||
		freeze.AppealURL != "https://appeals.example.test/updated" {
		t.Fatalf("updated freeze = %+v found=%v err=%v", freeze, found, err)
	}
}

func TestSetAccountFrozenReplayRemainsIdempotentAfterDeadline(t *testing.T) {
	ctx := context.Background()
	now := fixedNow()
	restrictions := &fakeRestrictionStore{}
	svc := NewService(Dependencies{
		Commands:     newMemoryCommandRepo(),
		Restrictions: restrictions,
		Now:          func() time.Time { return now },
	})
	req := SetAccountFrozenRequest{
		CommandMeta: CommandMeta{CommandID: "freeze-expiring", Actor: "ops", Reason: "review"},
		UserID:      1001,
		Frozen:      true,
		Until:       now.Add(time.Hour),
		AppealURL:   "https://appeals.example.test/expiring",
	}
	if _, err := svc.SetAccountFrozen(ctx, req); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	replayed, err := svc.SetAccountFrozen(ctx, req)
	if err != nil || !replayed.AlreadyExecuted || restrictions.setCalls != 1 {
		t.Fatalf("expired replay = %+v err=%v setCalls=%d", replayed, err, restrictions.setCalls)
	}
	stale := req
	stale.CommandID = "new-stale-freeze"
	if _, err := svc.SetAccountFrozen(ctx, stale); err == nil || restrictions.setCalls != 1 {
		t.Fatalf("new stale request err=%v setCalls=%d, want rejection without state mutation", err, restrictions.setCalls)
	}
}

func TestGrantPremiumDryRunExecuteAndIdempotency(t *testing.T) {
	ctx := context.Background()
	users := &fakeUsersService{users: map[int64]domain.User{
		1001: {ID: 1001, FirstName: "Alice"},
	}}
	notifier := &fakeUserNotifier{}
	svc := NewService(Dependencies{
		Commands:     newMemoryCommandRepo(),
		Users:        users,
		UserNotifier: notifier,
		Now:          fixedNow,
	})

	dry, err := svc.GrantPremium(ctx, GrantPremiumRequest{
		CommandMeta: CommandMeta{CommandID: "dry-premium", Actor: "ops", Reason: "test", DryRun: true},
		UserID:      1001,
		Months:      3,
	})
	if err != nil {
		t.Fatalf("dry-run premium: %v", err)
	}
	if !dry.DryRun || users.grantCalls != 0 || len(notifier.users) != 0 {
		t.Fatalf("dry=%+v grantCalls=%d notified=%v, want no mutation", dry, users.grantCalls, notifier.users)
	}

	req := GrantPremiumRequest{
		CommandMeta: CommandMeta{CommandID: "exec-premium", Actor: "ops", Reason: "grant"},
		UserID:      1001,
		Months:      2,
	}
	exec, err := svc.GrantPremium(ctx, req)
	if err != nil {
		t.Fatalf("execute premium: %v", err)
	}
	if exec.Status != string(domain.AdminCommandCompleted) || users.grantCalls != 1 || users.lastMonths != 2 || len(notifier.users) != 1 {
		t.Fatalf("exec=%+v grantCalls=%d months=%d notified=%v", exec, users.grantCalls, users.lastMonths, notifier.users)
	}
	again, err := svc.GrantPremium(ctx, req)
	if err != nil {
		t.Fatalf("duplicate premium: %v", err)
	}
	if !again.AlreadyExecuted || users.grantCalls != 1 || len(notifier.users) != 1 {
		t.Fatalf("again=%+v grantCalls=%d notified=%v, want idempotent replay", again, users.grantCalls, notifier.users)
	}
}

func TestSetVerifiedDryRunExecuteAndIdempotency(t *testing.T) {
	ctx := context.Background()
	users := &fakeUsersService{users: map[int64]domain.User{
		1001: {ID: 1001, FirstName: "Alice"},
	}}
	notifier := &fakeUserNotifier{}
	svc := NewService(Dependencies{
		Commands:     newMemoryCommandRepo(),
		Users:        users,
		UserNotifier: notifier,
		Now:          fixedNow,
	})

	dry, err := svc.SetVerified(ctx, SetVerifiedRequest{
		CommandMeta: CommandMeta{CommandID: "dry-verified", Actor: "ops", Reason: "test", DryRun: true},
		UserID:      1001,
		Verified:    true,
	})
	if err != nil {
		t.Fatalf("dry-run verified: %v", err)
	}
	if !dry.DryRun || users.verifiedCalls != 0 || len(notifier.users) != 0 {
		t.Fatalf("dry=%+v verifiedCalls=%d notified=%v, want no mutation", dry, users.verifiedCalls, notifier.users)
	}

	req := SetVerifiedRequest{
		CommandMeta: CommandMeta{CommandID: "exec-verified", Actor: "ops", Reason: "official"},
		UserID:      1001,
		Verified:    true,
	}
	exec, err := svc.SetVerified(ctx, req)
	if err != nil {
		t.Fatalf("execute verified: %v", err)
	}
	if exec.Status != string(domain.AdminCommandCompleted) || users.verifiedCalls != 1 || !users.users[1001].Verified || len(notifier.users) != 1 {
		t.Fatalf("exec=%+v verifiedCalls=%d user=%+v notified=%v", exec, users.verifiedCalls, users.users[1001], notifier.users)
	}
	again, err := svc.SetVerified(ctx, req)
	if err != nil {
		t.Fatalf("duplicate verified: %v", err)
	}
	if !again.AlreadyExecuted || users.verifiedCalls != 1 || len(notifier.users) != 1 {
		t.Fatalf("again=%+v verifiedCalls=%d notified=%v, want idempotent replay", again, users.verifiedCalls, notifier.users)
	}
}

func TestSetChannelVerifiedDryRunExecuteAndIdempotency(t *testing.T) {
	ctx := context.Background()
	channels := &fakeChannelsService{channels: map[int64]domain.Channel{
		2001: {ID: 2001, CreatorUserID: 1001, Title: "Ops Channel", Username: "ops", Broadcast: true},
	}}
	notifier := &fakeChannelNotifier{}
	svc := NewService(Dependencies{
		Commands:        newMemoryCommandRepo(),
		Channels:        channels,
		ChannelNotifier: notifier,
		Now:             fixedNow,
	})

	dry, err := svc.SetChannelVerified(ctx, SetChannelVerifiedRequest{
		CommandMeta: CommandMeta{CommandID: "dry-channel-verified", Actor: "ops", Reason: "test", DryRun: true},
		ChannelID:   2001,
		Verified:    true,
	})
	if err != nil {
		t.Fatalf("dry-run channel verified: %v", err)
	}
	if !dry.DryRun || channels.verifiedCalls != 0 || len(notifier.channels) != 0 {
		t.Fatalf("dry=%+v verifiedCalls=%d notified=%v, want no mutation", dry, channels.verifiedCalls, notifier.channels)
	}

	req := SetChannelVerifiedRequest{
		CommandMeta: CommandMeta{CommandID: "exec-channel-verified", Actor: "ops", Reason: "official"},
		ChannelID:   2001,
		Verified:    true,
	}
	exec, err := svc.SetChannelVerified(ctx, req)
	if err != nil {
		t.Fatalf("execute channel verified: %v", err)
	}
	if exec.Status != string(domain.AdminCommandCompleted) || channels.verifiedCalls != 1 || !channels.channels[2001].Verified || len(notifier.channels) != 1 {
		t.Fatalf("exec=%+v verifiedCalls=%d channel=%+v notified=%v", exec, channels.verifiedCalls, channels.channels[2001], notifier.channels)
	}
	if exec.TargetPeer.Type != domain.PeerTypeChannel || exec.TargetPeer.ID != 2001 || exec.TargetUserID != 0 {
		t.Fatalf("target user=%d peer=%+v, want channel target", exec.TargetUserID, exec.TargetPeer)
	}
	again, err := svc.SetChannelVerified(ctx, req)
	if err != nil {
		t.Fatalf("duplicate channel verified: %v", err)
	}
	if !again.AlreadyExecuted || channels.verifiedCalls != 1 || len(notifier.channels) != 1 {
		t.Fatalf("again=%+v verifiedCalls=%d notified=%v, want idempotent replay", again, channels.verifiedCalls, notifier.channels)
	}
}

func TestDeletePrivateMessagesUsesMessageServiceAndIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	messages := &fakeMessagesService{
		byID: []domain.Message{
			{OwnerUserID: 1001, ID: 11, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: 1002}},
			{OwnerUserID: 1001, ID: 12, Peer: domain.Peer{Type: domain.PeerTypeUser, ID: 1002}},
		},
	}
	svc := NewService(Dependencies{Commands: repo, Messages: messages, Now: fixedNow})
	req := DeletePrivateMessagesRequest{
		CommandMeta: CommandMeta{CommandID: "delete-1", Actor: "ops", Reason: "abuse"},
		OwnerUserID: 1001,
		Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: 1002},
		IDs:         []int{12, 11},
		Revoke:      true,
	}

	if _, err := svc.DeletePrivateMessages(ctx, req); err != nil {
		t.Fatalf("delete messages: %v", err)
	}
	if messages.deleteCalls != 1 || !reflect.DeepEqual(messages.lastDelete.IDs, []int{11, 12}) || !messages.lastDelete.Revoke {
		t.Fatalf("delete calls=%d req=%+v", messages.deleteCalls, messages.lastDelete)
	}
	if _, err := svc.DeletePrivateMessages(ctx, req); err != nil {
		t.Fatalf("duplicate delete messages: %v", err)
	}
	if messages.deleteCalls != 1 {
		t.Fatalf("duplicate delete calls=%d, want 1", messages.deleteCalls)
	}
}

func TestDeletePrivateMessagesRejectsMissingOnExecute(t *testing.T) {
	ctx := context.Background()
	svc := NewService(Dependencies{Commands: newMemoryCommandRepo(), Messages: &fakeMessagesService{}, Now: fixedNow})
	_, err := svc.DeletePrivateMessages(ctx, DeletePrivateMessagesRequest{
		CommandMeta: CommandMeta{CommandID: "delete-missing", Actor: "ops", Reason: "test"},
		OwnerUserID: 1001,
		Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: 1002},
		IDs:         []int{99},
	})
	if err == nil {
		t.Fatal("delete missing message err=nil, want error")
	}
}

func TestRevokeSessionsSpecifiedClosesRevokedAuthKey(t *testing.T) {
	ctx := context.Background()
	key := [8]byte{1, 2, 3}
	auth := &fakeAuthService{items: []domain.Authorization{
		{AuthKeyID: key, UserID: 1001, Hash: 555},
	}}
	revoker := &fakeRevoker{}
	svc := NewService(Dependencies{Commands: newMemoryCommandRepo(), Auth: auth, Revoker: revoker, Now: fixedNow})
	if _, err := svc.RevokeSessions(ctx, RevokeSessionsRequest{
		CommandMeta: CommandMeta{CommandID: "revoke-1", Actor: "ops", Reason: "lost device"},
		UserID:      1001,
		Hash:        555,
	}); err != nil {
		t.Fatalf("revoke sessions: %v", err)
	}
	if auth.resetHash != 555 || len(revoker.keys) != 1 || revoker.keys[0] != key {
		t.Fatalf("resetHash=%d revoked=%v", auth.resetHash, revoker.keys)
	}
}

func TestDeletePrivateHistoryLoopsUntilOffsetClears(t *testing.T) {
	ctx := context.Background()
	messages := &fakeMessagesService{historyOffsets: []int{1, 0}}
	svc := NewService(Dependencies{Commands: newMemoryCommandRepo(), Messages: messages, Now: fixedNow})
	res, err := svc.DeletePrivateHistory(ctx, DeletePrivateHistoryRequest{
		CommandMeta: CommandMeta{CommandID: "history-1", Actor: "ops", Reason: "clear"},
		OwnerUserID: 1001,
		Peer:        domain.Peer{Type: domain.PeerTypeUser, ID: 1002},
		MaxBatches:  5,
	})
	if err != nil {
		t.Fatalf("delete history: %v", err)
	}
	if messages.historyCalls != 2 || res.Details["has_more"] != false {
		t.Fatalf("historyCalls=%d result=%+v", messages.historyCalls, res)
	}
}

func fixedNow() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

type memoryCommandRepo struct {
	items map[string]domain.AdminCommand
}

func newMemoryCommandRepo() *memoryCommandRepo {
	return &memoryCommandRepo{items: map[string]domain.AdminCommand{}}
}

func (m *memoryCommandRepo) BeginCommand(_ context.Context, cmd domain.AdminCommand) (domain.AdminCommand, bool, error) {
	if existing, ok := m.items[cmd.CommandID]; ok {
		return existing, false, nil
	}
	m.items[cmd.CommandID] = cmd
	return cmd, true, nil
}

func (m *memoryCommandRepo) FinishCommand(_ context.Context, commandID string, status domain.AdminCommandStatus, resultJSON []byte, errorText string) (domain.AdminCommand, error) {
	cmd := m.items[commandID]
	cmd.Status = status
	cmd.ResultJSON = resultJSON
	cmd.Error = errorText
	m.items[commandID] = cmd
	return cmd, nil
}

type fakeBotService struct {
	token       string
	createCalls int
	deleteCalls int
	exportCalls int
}

func (f *fakeBotService) CreateBot(_ context.Context, _ int64, name, username string) (domain.User, string, error) {
	f.createCalls++
	return domain.User{ID: 2001, FirstName: name, Username: username, Bot: true}, f.token, nil
}

func (f *fakeBotService) DeleteBot(_ context.Context, botUserID int64) (domain.User, error) {
	f.deleteCalls++
	return domain.User{ID: botUserID, Bot: true, Deleted: true}, nil
}

func (f *fakeBotService) AdminExportBotToken(_ context.Context, botUserID int64) (string, error) {
	f.exportCalls++
	return f.token, nil
}

type fakeRestrictionStore struct {
	items    map[int64]domain.AccountFreeze
	setCalls int
}

func (f *fakeRestrictionStore) GetAccountFreeze(_ context.Context, userID int64) (domain.AccountFreeze, bool, error) {
	if f.items == nil {
		return domain.AccountFreeze{}, false, nil
	}
	r, ok := f.items[userID]
	return r, ok, nil
}

func (f *fakeRestrictionStore) SetAccountFreeze(_ context.Context, r domain.AccountFreeze) (domain.AccountFreeze, error) {
	if f.items == nil {
		f.items = map[int64]domain.AccountFreeze{}
	}
	f.setCalls++
	r.Version = f.items[r.UserID].Version + 1
	r.UpdatedAt = fixedNow()
	f.items[r.UserID] = r
	return r, nil
}

type fakeBatchRestrictionStore struct {
	fakeRestrictionStore
	requests [][]int64
}

func (f *fakeBatchRestrictionStore) GetAccountFreezes(_ context.Context, userIDs []int64) (map[int64]domain.AccountFreeze, error) {
	f.requests = append(f.requests, append([]int64(nil), userIDs...))
	out := make(map[int64]domain.AccountFreeze)
	for _, id := range userIDs {
		if freeze, ok := f.items[id]; ok && freeze.Frozen {
			out[id] = freeze
		}
	}
	return out, nil
}

type fakeAccountFreezeNotifier struct {
	items []domain.AccountFreeze
}

func (f *fakeAccountFreezeNotifier) NotifyAccountFreezeChanged(_ context.Context, freeze domain.AccountFreeze) error {
	f.items = append(f.items, freeze)
	return nil
}

type fakeMessagesService struct {
	byID           []domain.Message
	deleteCalls    int
	lastDelete     domain.DeleteMessagesRequest
	historyCalls   int
	historyOffsets []int
}

func (f *fakeMessagesService) GetMessages(_ context.Context, _ int64, _ []int) (domain.MessageList, error) {
	return domain.MessageList{Messages: f.byID}, nil
}

func (f *fakeMessagesService) GetHistory(_ context.Context, _ int64, _ domain.MessageFilter) (domain.MessageList, error) {
	return domain.MessageList{Messages: []domain.Message{{ID: 1}}}, nil
}

func (f *fakeMessagesService) DeleteMessages(_ context.Context, userID int64, req domain.DeleteMessagesRequest) (domain.DeleteMessagesResult, error) {
	f.deleteCalls++
	f.lastDelete = req
	return domain.DeleteMessagesResult{
		OwnerUserID: userID,
		Deleted: []domain.DeletedMessagesForUser{{
			UserID:     userID,
			MessageIDs: req.IDs,
			Event:      domain.UpdateEvent{Pts: 10, PtsCount: len(req.IDs)},
		}},
	}, nil
}

func (f *fakeMessagesService) DeleteHistory(_ context.Context, userID int64, _ domain.DeleteHistoryRequest) (domain.DeleteMessagesResult, error) {
	offset := 0
	if f.historyCalls < len(f.historyOffsets) {
		offset = f.historyOffsets[f.historyCalls]
	}
	f.historyCalls++
	return domain.DeleteMessagesResult{
		OwnerUserID: userID,
		Deleted: []domain.DeletedMessagesForUser{{
			UserID:     userID,
			MessageIDs: []int{f.historyCalls},
			Event:      domain.UpdateEvent{Pts: f.historyCalls, PtsCount: 1},
		}},
		Offset: offset,
	}, nil
}

type fakeAuthService struct {
	items     []domain.Authorization
	resetHash int64
}

func (f *fakeAuthService) ListAuthorizations(context.Context, int64) ([]domain.Authorization, error) {
	return f.items, nil
}

func (f *fakeAuthService) ResetAuthorization(_ context.Context, _ int64, hash int64) (domain.Authorization, bool, error) {
	f.resetHash = hash
	for _, a := range f.items {
		if a.Hash == hash {
			return a, true, nil
		}
	}
	return domain.Authorization{}, false, nil
}

func (f *fakeAuthService) ResetAuthorizations(_ context.Context, _ int64, keep [8]byte) ([]domain.Authorization, error) {
	out := make([]domain.Authorization, 0)
	for _, a := range f.items {
		if a.AuthKeyID != keep {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeRevoker struct {
	keys [][8]byte
}

func (f *fakeRevoker) RevokeAuthorizationAuthKey(_ context.Context, key [8]byte, _ int64) error {
	f.keys = append(f.keys, key)
	return nil
}

type fakeUsersService struct {
	users         map[int64]domain.User
	grantCalls    int
	lastMonths    int
	verifiedCalls int
}

func (f *fakeUsersService) AdminUser(_ context.Context, userID int64) (domain.User, bool, error) {
	u, ok := f.users[userID]
	return u, ok, nil
}

func (f *fakeUsersService) GrantPremium(_ context.Context, userID int64, months int) (domain.User, error) {
	f.grantCalls++
	f.lastMonths = months
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	if u.Bot {
		return domain.User{}, domain.ErrPremiumBotUnsupported
	}
	if months <= 0 {
		u.PremiumUntil = 0
	} else {
		u.PremiumUntil = int(fixedNow().AddDate(0, months, 0).Unix())
	}
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) SetVerified(_ context.Context, userID int64, verified bool) (domain.User, error) {
	f.verifiedCalls++
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	u.Verified = verified
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) SetScamFake(_ context.Context, userID int64, scam, fake bool) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	u.Scam = scam
	u.Fake = fake
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) SetSupport(_ context.Context, userID int64, support bool) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	u.Support = support
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) UpdateUsername(_ context.Context, userID int64, username string) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	u.Username = username
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) UpdateProfile(_ context.Context, userID int64, update domain.UserProfileUpdate) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	if update.HasFirstName {
		u.FirstName = update.FirstName
	}
	if update.HasLastName {
		u.LastName = update.LastName
	}
	if update.HasAbout {
		u.About = update.About
	}
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) SetPhone(_ context.Context, userID int64, phone string) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	for id, existing := range f.users {
		if id != userID && existing.Phone == phone {
			return domain.User{}, domain.ErrPhoneNumberOccupied
		}
	}
	u.Phone = phone
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) UpdateColor(_ context.Context, userID int64, forProfile bool, color domain.PeerColor) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	if forProfile {
		u.ProfileColor = color
	} else {
		u.Color = color
	}
	f.users[userID] = u
	return u, nil
}

func (f *fakeUsersService) UpdateEmojiStatus(_ context.Context, userID int64, status domain.UserEmojiStatus) (domain.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	f.users[userID] = u
	return u, nil
}

type fakeUserNotifier struct {
	users []int64
}

func (f *fakeUserNotifier) NotifyUserChanged(_ context.Context, u domain.User) error {
	f.users = append(f.users, u.ID)
	return nil
}

type fakeUserModerationNotifier struct {
	users []int64
}

func (f *fakeUserModerationNotifier) NotifyUserModerationFlagsChanged(_ context.Context, u domain.User) error {
	f.users = append(f.users, u.ID)
	return nil
}

type fakeChannelsService struct {
	channels      map[int64]domain.Channel
	verifiedCalls int
}

func (f *fakeChannelsService) GetChannelByID(_ context.Context, channelID int64) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	return ch, nil
}

func (f *fakeChannelsService) SetVerified(_ context.Context, channelID int64, verified bool) (domain.Channel, error) {
	f.verifiedCalls++
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	ch.Verified = verified
	f.channels[channelID] = ch
	return ch, nil
}

func (f *fakeChannelsService) SetScamFake(_ context.Context, channelID int64, scam, fake bool) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	ch.Scam = scam
	ch.Fake = fake
	f.channels[channelID] = ch
	return ch, nil
}

func (f *fakeChannelsService) AdminSetSettings(_ context.Context, channelID int64, patch domain.ChannelAdminSettings) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	if patch.Gigagroup != nil {
		ch.Gigagroup = *patch.Gigagroup
	}
	if patch.SlowmodeSeconds != nil {
		ch.SlowmodeSeconds = *patch.SlowmodeSeconds
	}
	f.channels[channelID] = ch
	return ch, nil
}

func (f *fakeChannelsService) AdminSetUsername(_ context.Context, channelID int64, username string) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	ch.Username = username
	f.channels[channelID] = ch
	return ch, nil
}

func (f *fakeChannelsService) AdminSetColor(_ context.Context, channelID int64, forProfile bool, color domain.ChannelPeerColor) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	if forProfile {
		ch.ProfileColor = color
	} else {
		ch.Color = color
	}
	f.channels[channelID] = ch
	return ch, nil
}

func (f *fakeChannelsService) AdminSetEmojiStatus(_ context.Context, channelID int64, status domain.ChannelEmojiStatus) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	ch.EmojiStatus = status
	f.channels[channelID] = ch
	return ch, nil
}

func (f *fakeChannelsService) AdminSetPhoto(_ context.Context, channelID int64, photo domain.Photo) (domain.Channel, error) {
	ch, ok := f.channels[channelID]
	if !ok {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	ch.PhotoID = photo.ID
	f.channels[channelID] = ch
	return ch, nil
}

type fakeChannelNotifier struct {
	channels []int64
}

func (f *fakeChannelNotifier) NotifyChannelChanged(_ context.Context, ch domain.Channel) error {
	f.channels = append(f.channels, ch.ID)
	return nil
}

// fakeCollectibleUsernamesService is an in-memory collectible lifecycle: enough
// state to observe occupancy, replay-by-command-key and the burned terminal
// state, which is exactly what the admin commands are expected to reason about.
type fakeCollectibleUsernamesService struct {
	assets        map[string]domain.CollectibleUsername
	log           map[int64][]domain.CollectibleUsernameTransfer
	commandKeys   map[string]int64
	nextID        int64
	mintCalls     int
	transferCalls int
	revokeCalls   int
	deleteCalls   int
}

func newFakeCollectibleUsernames() *fakeCollectibleUsernamesService {
	return &fakeCollectibleUsernamesService{
		assets:      map[string]domain.CollectibleUsername{},
		log:         map[int64][]domain.CollectibleUsernameTransfer{},
		commandKeys: map[string]int64{},
		nextID:      100,
	}
}

func (f *fakeCollectibleUsernamesService) Mint(_ context.Context, req domain.MintCollectibleUsernameRequest) (domain.CollectibleUsername, bool, error) {
	f.mintCalls++
	if id, ok := f.commandKeys[req.CommandKey]; ok && req.CommandKey != "" {
		return f.byID(id), false, nil
	}
	key := strings.ToLower(req.Username)
	if _, ok := f.assets[key]; ok {
		return domain.CollectibleUsername{}, false, domain.ErrUsernameOccupied
	}
	f.nextID++
	asset := domain.CollectibleUsername{
		ID: f.nextID, Username: req.Username, Status: domain.CollectibleUsernameStatusVault,
		PurchaseDate: req.PurchaseDate, Currency: req.Currency, Amount: req.Amount,
		CryptoCurrency: req.CryptoCurrency, CryptoAmount: req.CryptoAmount, URL: req.URL,
		Version: 1,
	}
	if req.Owner.Type != "" {
		asset.Status = domain.CollectibleUsernameStatusOwned
		asset.Owner = req.Owner
		asset.OriginalOwner = req.Owner
	}
	f.assets[key] = asset
	f.commandKeys[req.CommandKey] = asset.ID
	f.log[asset.ID] = append(f.log[asset.ID], domain.CollectibleUsernameTransfer{
		ID: asset.ID, CollectibleID: asset.ID, Kind: domain.CollectibleUsernameKindMint,
		To: req.Owner, Actor: req.Actor, Reason: req.Reason, CommandKey: req.CommandKey,
	})
	return asset, true, nil
}

func (f *fakeCollectibleUsernamesService) Transfer(_ context.Context, req domain.TransferCollectibleUsernameRequest) (domain.CollectibleUsername, bool, error) {
	f.transferCalls++
	key := strings.ToLower(req.Username)
	asset, ok := f.assets[key]
	if !ok {
		return domain.CollectibleUsername{}, false, domain.ErrCollectibleUsernameNotFound
	}
	if asset.Status == domain.CollectibleUsernameStatusBurned {
		return domain.CollectibleUsername{}, false, domain.ErrCollectibleUsernameBurned
	}
	if asset.Owned() && asset.Owner == req.To {
		return asset, false, nil
	}
	asset.Status = domain.CollectibleUsernameStatusOwned
	asset.Owner = req.To
	if asset.OriginalOwner.Type == "" {
		asset.OriginalOwner = req.To
	}
	asset.TransferCount++
	asset.Version++
	f.assets[key] = asset
	return asset, true, nil
}

func (f *fakeCollectibleUsernamesService) Delete(_ context.Context, req domain.DeleteCollectibleUsernameRequest) (bool, error) {
	f.deleteCalls++
	key := strings.ToLower(req.Username)
	asset, ok := f.assets[key]
	if !ok {
		return false, nil
	}
	if asset.Status == domain.CollectibleUsernameStatusBurned {
		return false, nil
	}
	delete(f.assets, key)
	return true, nil
}

func (f *fakeCollectibleUsernamesService) Revoke(_ context.Context, req domain.RevokeCollectibleUsernameRequest) (domain.CollectibleUsername, bool, error) {
	f.revokeCalls++
	key := strings.ToLower(req.Username)
	asset, ok := f.assets[key]
	if !ok {
		return domain.CollectibleUsername{}, false, domain.ErrCollectibleUsernameNotFound
	}
	if asset.Status == domain.CollectibleUsernameStatusBurned {
		return domain.CollectibleUsername{}, false, domain.ErrCollectibleUsernameBurned
	}
	asset.Owner = domain.Peer{}
	asset.Status = domain.CollectibleUsernameStatusVault
	if req.Burn {
		asset.Status = domain.CollectibleUsernameStatusBurned
	}
	asset.Version++
	f.assets[key] = asset
	return asset, true, nil
}

func (f *fakeCollectibleUsernamesService) Collectible(_ context.Context, username string) (domain.CollectibleUsername, error) {
	asset, ok := f.assets[strings.ToLower(username)]
	if !ok {
		return domain.CollectibleUsername{}, domain.ErrCollectibleUsernameNotFound
	}
	return asset, nil
}

func (f *fakeCollectibleUsernamesService) List(_ context.Context, filter domain.CollectibleUsernameFilter) ([]domain.CollectibleUsername, error) {
	out := make([]domain.CollectibleUsername, 0, len(f.assets))
	for _, asset := range f.assets {
		if filter.Status != "" && asset.Status != filter.Status {
			continue
		}
		if filter.BeforeID != 0 && asset.ID >= filter.BeforeID {
			continue
		}
		out = append(out, asset)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (f *fakeCollectibleUsernamesService) Transfers(_ context.Context, collectibleID int64, _ int) ([]domain.CollectibleUsernameTransfer, error) {
	return f.log[collectibleID], nil
}

func (f *fakeCollectibleUsernamesService) byID(id int64) domain.CollectibleUsername {
	for _, asset := range f.assets {
		if asset.ID == id {
			return asset
		}
	}
	return domain.CollectibleUsername{}
}

func TestMintCollectibleUsernameDryRunExecuteAndIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	usernames := newFakeCollectibleUsernames()
	svc := NewService(Dependencies{Commands: repo, Usernames: usernames, Now: fixedNow})

	dry, err := svc.MintCollectibleUsername(ctx, MintCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "dry-mint", Actor: "ops", Reason: "fragment import", DryRun: true},
		Username:    "@Durov", OwnerUserID: 1001, Currency: domain.CollectibleCurrencyTON,
		Amount: 250_000_000_000, CryptoCurrency: domain.CollectibleCryptoCurrencyTON, CryptoAmount: 250_000_000_000,
	})
	if err != nil {
		t.Fatalf("dry-run mint: %v", err)
	}
	if !dry.DryRun || dry.Status != string(domain.AdminCommandCompleted) || usernames.mintCalls != 0 {
		t.Fatalf("dry-run result=%+v mintCalls=%d, want validation without mutation", dry, usernames.mintCalls)
	}
	if dry.Details["username"] != "Durov" || dry.Details["purchase_date"] != fixedNow().Format(time.RFC3339) {
		t.Fatalf("dry-run details=%+v, want a normalised name and a stamped purchase date", dry.Details)
	}

	execReq := MintCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "exec-mint", Actor: "ops", Reason: "fragment import"},
		Username:    "Durov", OwnerUserID: 1001, Currency: domain.CollectibleCurrencyTON,
		Amount: 250_000_000_000, CryptoCurrency: domain.CollectibleCryptoCurrencyTON, CryptoAmount: 250_000_000_000,
	}
	exec, err := svc.MintCollectibleUsername(ctx, execReq)
	if err != nil {
		t.Fatalf("execute mint: %v", err)
	}
	if exec.Status != string(domain.AdminCommandCompleted) || usernames.mintCalls != 1 ||
		exec.Details["status"] != string(domain.CollectibleUsernameStatusOwned) {
		t.Fatalf("execute result=%+v mintCalls=%d", exec, usernames.mintCalls)
	}

	again, err := svc.MintCollectibleUsername(ctx, execReq)
	if err != nil {
		t.Fatalf("replay mint: %v", err)
	}
	if !again.AlreadyExecuted || usernames.mintCalls != 1 {
		t.Fatalf("replay result=%+v mintCalls=%d, want idempotent replay", again, usernames.mintCalls)
	}

	occupied, err := svc.MintCollectibleUsername(ctx, MintCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "exec-mint-2", Actor: "ops", Reason: "duplicate"},
		Username:    "durov", Currency: domain.CollectibleCurrencyUSD, Amount: 1,
	})
	if err == nil || !strings.Contains(err.Error(), CodeUsernameOccupied) {
		t.Fatalf("duplicate mint err=%v, want %s", err, CodeUsernameOccupied)
	}
	if occupied.Status != string(domain.AdminCommandFailed) || usernames.mintCalls != 1 {
		t.Fatalf("duplicate result=%+v mintCalls=%d, want a journalled failure without mutation", occupied, usernames.mintCalls)
	}
}

func TestMintCollectibleUsernameValidatesBeforeJournallingCommand(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	usernames := newFakeCollectibleUsernames()
	svc := NewService(Dependencies{Commands: repo, Usernames: usernames, Now: fixedNow})

	cases := []struct {
		name string
		req  MintCollectibleUsernameRequest
		code string
	}{
		{
			name: "short username",
			req: MintCollectibleUsernameRequest{
				CommandMeta: CommandMeta{CommandID: "bad-1", Actor: "ops", Reason: "invalid"},
				Username:    "ab", Currency: domain.CollectibleCurrencyUSD, Amount: 1,
			},
			code: CodeUsernameInvalid,
		},
		{
			name: "unsupported currency",
			req: MintCollectibleUsernameRequest{
				CommandMeta: CommandMeta{CommandID: "bad-2", Actor: "ops", Reason: "invalid"},
				Username:    "durov", Currency: "EUR", Amount: 1,
			},
			code: CodeCollectibleCurrencyInvalid,
		},
		{
			name: "crypto amount without currency",
			req: MintCollectibleUsernameRequest{
				CommandMeta: CommandMeta{CommandID: "bad-3", Actor: "ops", Reason: "invalid"},
				Username:    "durov", Currency: domain.CollectibleCurrencyUSD, Amount: 1, CryptoAmount: 5,
			},
			code: CodeCollectibleCurrencyInvalid,
		},
	}
	for _, item := range cases {
		if _, err := svc.MintCollectibleUsername(ctx, item.req); err == nil || !strings.Contains(err.Error(), item.code) {
			t.Fatalf("%s err=%v, want %s", item.name, err, item.code)
		}
		if _, journalled := repo.items[item.req.CommandID]; journalled {
			t.Fatalf("%s journalled a rejected command", item.name)
		}
	}
	if usernames.mintCalls != 0 {
		t.Fatalf("rejected requests reached the lifecycle: mintCalls=%d", usernames.mintCalls)
	}
}

func TestTransferCollectibleUsernameRequiresRecipientAndRejectsBurned(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	usernames := newFakeCollectibleUsernames()
	svc := NewService(Dependencies{Commands: repo, Usernames: usernames, Now: fixedNow})
	if _, _, err := usernames.Mint(ctx, domain.MintCollectibleUsernameRequest{
		Username: "durov", Currency: domain.CollectibleCurrencyUSD, Amount: 1, CommandKey: "seed",
	}); err != nil {
		t.Fatalf("seed mint: %v", err)
	}

	if _, err := svc.TransferCollectibleUsername(ctx, TransferCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "t-none", Actor: "ops", Reason: "sold"}, Username: "durov",
	}); err == nil || !strings.Contains(err.Error(), "to_user_id") {
		t.Fatalf("transfer without recipient err=%v", err)
	}
	if _, err := svc.TransferCollectibleUsername(ctx, TransferCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "t-both", Actor: "ops", Reason: "sold"},
		Username:    "durov", ToUserID: 1001, ToChannelID: 2002,
	}); err == nil {
		t.Fatal("transfer accepted two recipients")
	}

	dry, err := svc.TransferCollectibleUsername(ctx, TransferCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "t-dry", Actor: "ops", Reason: "sold", DryRun: true},
		Username:    "durov", ToUserID: 1001,
	})
	if err != nil {
		t.Fatalf("dry-run transfer: %v", err)
	}
	if usernames.transferCalls != 0 || dry.Details["would_change"] != true {
		t.Fatalf("dry-run transfer result=%+v transferCalls=%d", dry, usernames.transferCalls)
	}

	if _, err := svc.TransferCollectibleUsername(ctx, TransferCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "t-exec", Actor: "ops", Reason: "sold"},
		Username:    "durov", ToUserID: 1001,
	}); err != nil {
		t.Fatalf("execute transfer: %v", err)
	}
	if usernames.transferCalls != 1 {
		t.Fatalf("transferCalls=%d, want one mutation", usernames.transferCalls)
	}

	if _, err := svc.RevokeCollectibleUsername(ctx, RevokeCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "burn-1", Actor: "ops", Reason: "fraud"},
		Username:    "durov", Burn: true,
	}); err != nil {
		t.Fatalf("burn: %v", err)
	}
	if _, err := svc.TransferCollectibleUsername(ctx, TransferCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "t-burned", Actor: "ops", Reason: "sold"},
		Username:    "durov", ToUserID: 1002,
	}); err == nil || !strings.Contains(err.Error(), CodeCollectibleBurned) {
		t.Fatalf("transfer of a burned asset err=%v, want %s", err, CodeCollectibleBurned)
	}
	if _, err := svc.TransferCollectibleUsername(ctx, TransferCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "t-missing", Actor: "ops", Reason: "sold"},
		Username:    "nobody_holds_this", ToUserID: 1002,
	}); err == nil || !strings.Contains(err.Error(), CodeCollectibleNotFound) {
		t.Fatalf("transfer of a missing asset err=%v, want %s", err, CodeCollectibleNotFound)
	}
}

func TestRevokeCollectibleUsernameRejectsVaultAssetWithoutBurn(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCommandRepo()
	usernames := newFakeCollectibleUsernames()
	svc := NewService(Dependencies{Commands: repo, Usernames: usernames, Now: fixedNow})
	if _, _, err := usernames.Mint(ctx, domain.MintCollectibleUsernameRequest{
		Username: "durov", Currency: domain.CollectibleCurrencyUSD, Amount: 1, CommandKey: "seed",
	}); err != nil {
		t.Fatalf("seed mint: %v", err)
	}

	if _, err := svc.RevokeCollectibleUsername(ctx, RevokeCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "rv-vault", Actor: "ops", Reason: "nothing to take"},
		Username:    "durov",
	}); err == nil || !strings.Contains(err.Error(), CodeCollectibleNotOwned) {
		t.Fatalf("revoke of a vault asset err=%v, want %s", err, CodeCollectibleNotOwned)
	}
	if usernames.revokeCalls != 0 {
		t.Fatalf("revokeCalls=%d, want no mutation", usernames.revokeCalls)
	}

	burnDry, err := svc.RevokeCollectibleUsername(ctx, RevokeCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "rv-burn-dry", Actor: "ops", Reason: "fraud", DryRun: true},
		Username:    "durov", Burn: true,
	})
	if err != nil {
		t.Fatalf("dry-run burn: %v", err)
	}
	if usernames.revokeCalls != 0 || burnDry.Details["burn"] != true {
		t.Fatalf("dry-run burn result=%+v revokeCalls=%d", burnDry, usernames.revokeCalls)
	}
}

func TestCollectibleUsernameByIDUsesKeysetFallback(t *testing.T) {
	ctx := context.Background()
	usernames := newFakeCollectibleUsernames()
	svc := NewService(Dependencies{Commands: newMemoryCommandRepo(), Usernames: usernames, Now: fixedNow})
	first, _, err := usernames.Mint(ctx, domain.MintCollectibleUsernameRequest{
		Username: "durov", Currency: domain.CollectibleCurrencyUSD, Amount: 1, CommandKey: "seed-1",
	})
	if err != nil {
		t.Fatalf("seed mint: %v", err)
	}
	if _, _, err := usernames.Mint(ctx, domain.MintCollectibleUsernameRequest{
		Username: "telegram", Currency: domain.CollectibleCurrencyUSD, Amount: 1, CommandKey: "seed-2",
	}); err != nil {
		t.Fatalf("seed mint: %v", err)
	}

	got, err := svc.CollectibleUsernameByID(ctx, first.ID)
	if err != nil || got.ID != first.ID || got.Username != "durov" {
		t.Fatalf("CollectibleUsernameByID(%d) = %+v err=%v", first.ID, got, err)
	}
	if _, err := svc.CollectibleUsernameByID(ctx, first.ID-1); !errors.Is(err, domain.ErrCollectibleUsernameNotFound) {
		t.Fatalf("missing id err=%v, want ErrCollectibleUsernameNotFound", err)
	}
}

func TestCollectibleCommandsRequireConfiguredDependencies(t *testing.T) {
	ctx := context.Background()
	svc := NewService(Dependencies{Commands: newMemoryCommandRepo(), Now: fixedNow})
	if _, err := svc.MintCollectibleUsername(ctx, MintCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "c-1", Actor: "ops", Reason: "x"},
		Username:    "durov", Currency: domain.CollectibleCurrencyUSD, Amount: 1,
	}); err == nil || !strings.Contains(err.Error(), "collectible username dependency") {
		t.Fatalf("mint without dependency err=%v", err)
	}
	if _, err := svc.CollectibleUsernames(ctx, domain.CollectibleUsernameFilter{}); err == nil {
		t.Fatal("listing without dependency succeeded")
	}
}

// TestDeleteCollectibleUsernameCommand covers the hard-delete command: the
// journal captures what was removed before the record disappears, a dry-run
// mutates nothing, and a burned asset is refused.
func TestDeleteCollectibleUsernameCommand(t *testing.T) {
	ctx := context.Background()
	usernames := newFakeCollectibleUsernames()
	repo := newMemoryCommandRepo()
	svc := NewService(Dependencies{Commands: repo, Usernames: usernames, Now: fixedNow})
	holder := domain.Peer{Type: domain.PeerTypeUser, ID: 8801}
	if _, _, err := usernames.Mint(ctx, domain.MintCollectibleUsernameRequest{
		Username: "wrongname", Owner: holder, Currency: domain.CollectibleCurrencyStars, Amount: 100,
	}); err != nil {
		t.Fatalf("seed mint: %v", err)
	}

	dry, err := svc.DeleteCollectibleUsername(ctx, DeleteCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "del-dry", Actor: "ops", Reason: "mistake", DryRun: true},
		Username:    "@wrongname",
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || usernames.deleteCalls != 0 {
		t.Fatalf("dry run mutated: result=%+v calls=%d", dry, usernames.deleteCalls)
	}
	if dry.Details["previous_owner_id"] != "8801" {
		t.Fatalf("dry run details = %+v, want the holder captured", dry.Details)
	}

	exec, err := svc.DeleteCollectibleUsername(ctx, DeleteCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "del-exec", Actor: "ops", Reason: "mistake"},
		Username:    "wrongname",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if usernames.deleteCalls != 1 || exec.Details["deleted"] != true {
		t.Fatalf("exec = %+v calls=%d", exec, usernames.deleteCalls)
	}
	if exec.Details["previous_status"] != string(domain.CollectibleUsernameStatusOwned) {
		t.Fatalf("journal lost the pre-delete state: %+v", exec.Details)
	}

	// Replaying the same command id must not touch the store again.
	if _, err := svc.DeleteCollectibleUsername(ctx, DeleteCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "del-exec", Actor: "ops", Reason: "mistake"},
		Username:    "wrongname",
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if usernames.deleteCalls != 1 {
		t.Fatalf("replay called the store again: calls=%d", usernames.deleteCalls)
	}

	// A burned asset is history: it is released by re-issuing the name, not deleted.
	if _, _, err := usernames.Mint(ctx, domain.MintCollectibleUsernameRequest{
		Username: "burnedname", Owner: holder, Currency: domain.CollectibleCurrencyStars, Amount: 100,
	}); err != nil {
		t.Fatalf("seed burned mint: %v", err)
	}
	if _, _, err := usernames.Revoke(ctx, domain.RevokeCollectibleUsernameRequest{
		Username: "burnedname", Burn: true,
	}); err != nil {
		t.Fatalf("seed burn: %v", err)
	}
	if _, err := svc.DeleteCollectibleUsername(ctx, DeleteCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "del-burned", Actor: "ops", Reason: "cleanup"},
		Username:    "burnedname",
	}); err == nil || !strings.Contains(err.Error(), CodeCollectibleBurned) {
		t.Fatalf("delete of burned asset err = %v, want %s", err, CodeCollectibleBurned)
	}

	// A short name is rejected before a command is journalled at all.
	if _, err := svc.DeleteCollectibleUsername(ctx, DeleteCollectibleUsernameRequest{
		CommandMeta: CommandMeta{CommandID: "del-short", Actor: "ops", Reason: "cleanup"},
		Username:    "no",
	}); err == nil {
		t.Fatalf("delete of invalid name = nil error, want rejection")
	}
}
