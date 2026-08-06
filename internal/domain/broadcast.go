package domain

import (
	"errors"
	"time"
)

// BroadcastTargetMode selects who a broadcast's recipients are.
type BroadcastTargetMode string

const (
	// BroadcastTargetAll snapshots every non-bot, non-system account at
	// creation time (mirrors the exclusion cmd/telesrv-admin's CountAccounts
	// already applies: real users only, not @BotFather/@Stickers/@ChatBot/777000
	// itself).
	BroadcastTargetAll BroadcastTargetMode = "all"
	// BroadcastTargetSelected sends only to the operator-picked user list
	// carried on the create request.
	BroadcastTargetSelected BroadcastTargetMode = "selected"
)

// BroadcastRecipientStatus is one recipient row's delivery state.
type BroadcastRecipientStatus string

const (
	BroadcastRecipientPending BroadcastRecipientStatus = "pending"
	BroadcastRecipientSent    BroadcastRecipientStatus = "sent"
	// BroadcastRecipientFailed is terminal: MaxBroadcastRecipientAttempts was
	// reached, so the worker stops retrying this row. A blocked or deleted
	// recipient must not spin forever alongside everyone else's real deliveries.
	BroadcastRecipientFailed BroadcastRecipientStatus = "failed"
)

// MaxBroadcastRecipientAttempts bounds retries per recipient before the
// worker gives up and marks the row permanently failed.
const MaxBroadcastRecipientAttempts = 5

// Broadcast is one admin-triggered system message campaign, sent from
// OfficialSystemUserID (777000) to every recipient snapshotted into
// broadcast_recipients at creation time. SentCount/FailedCount are derived
// from the recipient rows at read time, not stored, so they can never drift.
type Broadcast struct {
	ID          int64
	Message     string
	TargetMode  BroadcastTargetMode
	TotalCount  int
	SentCount   int
	FailedCount int
	CreatedBy   string
	CreatedAt   time.Time
}

// BroadcastRecipient is one durable outbox row: one user's delivery state
// for one broadcast.
type BroadcastRecipient struct {
	ID          int64
	BroadcastID int64
	UserID      int64
	Status      BroadcastRecipientStatus
	Attempts    int
	LastError   string
	SentAt      *time.Time
}

var (
	ErrBroadcastInvalid      = errors.New("broadcast invalid")
	ErrBroadcastMessageEmpty = errors.New("broadcast message is empty")
	ErrBroadcastNoRecipients = errors.New("broadcast has no recipients")
	ErrBroadcastNotFound     = errors.New("broadcast not found")
)
