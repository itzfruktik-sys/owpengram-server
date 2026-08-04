package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"telesrv/internal/domain"
	"telesrv/internal/store/postgres/sqlcgen"
)

// messageBoxRefKey/channelMessageRefKey are the ref_key encodings used by
// media_references rows registered from the private-mailbox and channel
// message write paths, respectively. Kept as named helpers so the add and
// remove sides can never drift apart on format.
func messageBoxRefKey(ownerUserID int64, boxID int) string {
	return fmt.Sprintf("user:%d:box:%d", ownerUserID, boxID)
}

func channelMessageRefKey(channelID int64, messageID int) string {
	return fmt.Sprintf("channel:%d:msg:%d", channelID, messageID)
}

// addMediaReferencesTx registers every document/photo embedded in media as
// referenced by refKind/refKey, clearing orphaned_at on each if it had been
// set by an earlier removal. Must run in the same transaction as the write
// that creates the reference (message send/edit).
func addMediaReferencesTx(ctx context.Context, tx sqlcgen.DBTX, media *domain.MessageMedia, refKind domain.MediaRefKind, refKey string) error {
	targets := domain.ExtractMediaRefTargets(media)
	if len(targets) == 0 {
		return nil
	}
	q := sqlcgen.New(tx)
	for _, t := range targets {
		if err := q.InsertMediaReference(ctx, sqlcgen.InsertMediaReferenceParams{
			MediaKind: string(t.Kind),
			MediaID:   t.ID,
			RefKind:   string(refKind),
			RefKey:    refKey,
		}); err != nil {
			return fmt.Errorf("insert media reference: %w", err)
		}
		var clearErr error
		switch t.Kind {
		case domain.MediaKindDocument:
			clearErr = q.ClearDocumentOrphan(ctx, t.ID)
		case domain.MediaKindPhoto:
			clearErr = q.ClearPhotoOrphan(ctx, t.ID)
		}
		if clearErr != nil {
			return fmt.Errorf("clear media orphan: %w", clearErr)
		}
	}
	return nil
}

// removeMediaReferencesByKeyTx drops every media_references row registered
// under refKind/refKey (no need to know which document/photo ids those were
// -- the delete finds them) and, for each one that becomes fully
// unreferenced as a result, marks it orphaned so the storage retention
// sweep can consider it once old enough. Must run in the same transaction
// as the write that removes the reference (a message being soft-deleted).
func removeMediaReferencesByKeyTx(ctx context.Context, tx sqlcgen.DBTX, refKind domain.MediaRefKind, refKey string) error {
	rows, err := tx.Query(ctx, `
DELETE FROM media_references
WHERE ref_kind = $1 AND ref_key = $2
RETURNING media_kind, media_id`, string(refKind), refKey)
	if err != nil {
		return fmt.Errorf("remove media references: %w", err)
	}
	type removedRef struct {
		kind string
		id   int64
	}
	var removed []removedRef
	for rows.Next() {
		var r removedRef
		if err := rows.Scan(&r.kind, &r.id); err != nil {
			rows.Close()
			return fmt.Errorf("scan removed media reference: %w", err)
		}
		removed = append(removed, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("remove media references: %w", err)
	}
	rows.Close()

	q := sqlcgen.New(tx)
	for _, r := range removed {
		var orphanErr error
		switch domain.MediaKind(r.kind) {
		case domain.MediaKindDocument:
			orphanErr = q.OrphanDocumentIfUnreferenced(ctx, r.id)
		case domain.MediaKindPhoto:
			orphanErr = q.OrphanPhotoIfUnreferenced(ctx, r.id)
		}
		if orphanErr != nil {
			return fmt.Errorf("orphan check media: %w", orphanErr)
		}
	}
	return nil
}

// ---- storage retention sweep ----

// ListOrphanedDocumentIDsOlderThan returns document ids whose orphaned_at is
// set and older than cutoff, oldest first, up to limit.
func (s *MediaStore) ListOrphanedDocumentIDsOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.q.ListOrphanedDocumentIDsOlderThan(ctx, sqlcgen.ListOrphanedDocumentIDsOlderThanParams{
		Cutoff:     pgtype.Timestamptz{Time: cutoff, Valid: true},
		BatchLimit: int32(limit),
	})
}

// ListOrphanedPhotoIDsOlderThan returns photo ids whose orphaned_at is set
// and older than cutoff, oldest first, up to limit.
func (s *MediaStore) ListOrphanedPhotoIDsOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.q.ListOrphanedPhotoIDsOlderThan(ctx, sqlcgen.ListOrphanedPhotoIDsOlderThanParams{
		Cutoff:     pgtype.Timestamptz{Time: cutoff, Valid: true},
		BatchLimit: int32(limit),
	})
}

// CountFileBlobRefs reports how many file_blobs rows still point at
// (backend, objectKey) -- the caller must not physically delete the object
// from that backend while this is > 0 (content-addressed storage: the same
// object can be shared by multiple documents/photos).
func (s *MediaStore) CountFileBlobRefs(ctx context.Context, backend, objectKey string) (int, error) {
	n, err := s.q.CountFileBlobRefs(ctx, sqlcgen.CountFileBlobRefsParams{Backend: backend, ObjectKey: objectKey})
	return int(n), err
}

// DeleteDocumentAndBlobs deletes a document row and every file_blobs row it
// owns (main body + thumbnail variants), returning what was deleted so the
// caller can physically remove each object from its backend once confirming
// (via CountFileBlobRefs, after this call) no other row still needs it.
// Assumes the document is already orphaned -- does not check references.
func (s *MediaStore) DeleteDocumentAndBlobs(ctx context.Context, id int64) ([]domain.FileBlob, error) {
	var blobs []domain.FileBlob
	err := withTx(ctx, s.db, "delete document and blobs", func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		rows, err := qtx.ListFileBlobsByLocationPrefix(ctx, sqlcgen.ListFileBlobsByLocationPrefixParams{
			ExactKey:      fmt.Sprintf("doc:%d", id),
			PrefixPattern: fmt.Sprintf("doc:%d:%%", id),
		})
		if err != nil {
			return fmt.Errorf("list document blobs: %w", err)
		}
		for _, r := range rows {
			blobs = append(blobs, domain.FileBlob{
				LocationKey: r.LocationKey, Backend: domain.MediaBackend(r.Backend), ObjectKey: r.ObjectKey, Size: r.Size,
			})
			if err := qtx.DeleteFileBlobRow(ctx, r.LocationKey); err != nil {
				return fmt.Errorf("delete file blob row: %w", err)
			}
		}
		if err := qtx.DeleteDocumentRow(ctx, id); err != nil {
			return fmt.Errorf("delete document row: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.documents.remove(id)
	return blobs, nil
}

// DeletePhotoAndBlobs deletes a photo row and every file_blobs row it owns
// (one per rendition size), returning what was deleted so the caller can
// physically remove each object from its backend once confirming (via
// CountFileBlobRefs, after this call) no other row still needs it. Assumes
// the photo is already orphaned -- does not check references.
func (s *MediaStore) DeletePhotoAndBlobs(ctx context.Context, id int64) ([]domain.FileBlob, error) {
	var blobs []domain.FileBlob
	err := withTx(ctx, s.db, "delete photo and blobs", func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		rows, err := qtx.ListFileBlobsByLocationPrefix(ctx, sqlcgen.ListFileBlobsByLocationPrefixParams{
			ExactKey:      fmt.Sprintf("photo:%d", id),
			PrefixPattern: fmt.Sprintf("photo:%d:%%", id),
		})
		if err != nil {
			return fmt.Errorf("list photo blobs: %w", err)
		}
		for _, r := range rows {
			blobs = append(blobs, domain.FileBlob{
				LocationKey: r.LocationKey, Backend: domain.MediaBackend(r.Backend), ObjectKey: r.ObjectKey, Size: r.Size,
			})
			if err := qtx.DeleteFileBlobRow(ctx, r.LocationKey); err != nil {
				return fmt.Errorf("delete file blob row: %w", err)
			}
		}
		if err := qtx.DeletePhotoRow(ctx, id); err != nil {
			return fmt.Errorf("delete photo row: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return blobs, nil
}
