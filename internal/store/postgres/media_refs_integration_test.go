package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"telesrv/internal/domain"
)

// TestMediaReferenceOrphanTransitions proves the core storage-retention
// safety invariant: a document's orphaned_at is set only once every
// reference to it is gone, and cleared the instant a new one appears --
// so the retention sweep never targets media still visible in a
// conversation, regardless of how many places reference it.
func TestMediaReferenceOrphanTransitions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewMediaStore(pool)

	const docID = int64(9100000000000000101)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM media_references WHERE media_kind = 'document' AND media_id = $1`, docID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM documents WHERE id = $1`, docID)
	})

	if err := s.PutDocument(ctx, domain.Document{ID: docID, MimeType: "text/plain", Size: 10}); err != nil {
		t.Fatalf("put document: %v", err)
	}

	media := &domain.MessageMedia{Kind: domain.MessageMediaKindDocument, Document: &domain.Document{ID: docID}}

	// A freshly created document has no orphaned_at yet either way -- it's
	// simply unreferenced until a message send registers the first
	// reference, at which point normal tracking takes over.
	orphaned, err := documentOrphanedAt(ctx, pool, docID)
	if err != nil {
		t.Fatalf("query orphaned_at: %v", err)
	}
	if orphaned {
		t.Fatal("expected a freshly inserted document to not be marked orphaned yet")
	}

	// Adding a reference (as if a message carrying it was sent) clears it.
	mustAddRef(t, pool, media, domain.MediaRefKindMessageBox, "user:1:box:1")
	if orphaned, err := documentOrphanedAt(ctx, pool, docID); err != nil || orphaned {
		t.Fatalf("expected referenced document to not be orphaned, orphaned=%v err=%v", orphaned, err)
	}

	// A second, independent reference (e.g. forwarded to another box).
	mustAddRef(t, pool, media, domain.MediaRefKindMessageBox, "user:2:box:5")

	// Removing only one of the two references must NOT orphan the document.
	mustRemoveRefsByKey(t, pool, domain.MediaRefKindMessageBox, "user:1:box:1")
	if orphaned, err := documentOrphanedAt(ctx, pool, docID); err != nil || orphaned {
		t.Fatalf("expected document with a remaining reference to survive, orphaned=%v err=%v", orphaned, err)
	}

	// Removing the last reference orphans it.
	mustRemoveRefsByKey(t, pool, domain.MediaRefKindMessageBox, "user:2:box:5")
	if orphaned, err := documentOrphanedAt(ctx, pool, docID); err != nil || !orphaned {
		t.Fatalf("expected document with no remaining reference to be orphaned, orphaned=%v err=%v", orphaned, err)
	}

	// A reference reappearing after orphaning (e.g. re-sent) clears it again.
	mustAddRef(t, pool, media, domain.MediaRefKindMessageBox, "user:3:box:9")
	if orphaned, err := documentOrphanedAt(ctx, pool, docID); err != nil || orphaned {
		t.Fatalf("expected re-referenced document to no longer be orphaned, orphaned=%v err=%v", orphaned, err)
	}
}

func mustAddRef(t *testing.T, pool *pgxpool.Pool, media *domain.MessageMedia, refKind domain.MediaRefKind, refKey string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := addMediaReferencesTx(ctx, tx, media, refKind, refKey); err != nil {
		t.Fatalf("add media reference: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func mustRemoveRefsByKey(t *testing.T, pool *pgxpool.Pool, refKind domain.MediaRefKind, refKey string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := removeMediaReferencesByKeyTx(ctx, tx, refKind, refKey); err != nil {
		t.Fatalf("remove media references: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func documentOrphanedAt(ctx context.Context, pool *pgxpool.Pool, id int64) (bool, error) {
	var orphaned bool
	err := pool.QueryRow(ctx, `SELECT orphaned_at IS NOT NULL FROM documents WHERE id = $1`, id).Scan(&orphaned)
	return orphaned, err
}
