package files

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"telesrv/internal/domain"
)

// mediaRetentionStore is implemented by store.MediaStore backends that
// support the storage retention sweep (currently only the Postgres store).
// A type assertion, not a MediaStore interface method, keeps these
// admin/maintenance-only queries out of the hot RPC-facing interface --
// same convention as photoBatchStore above.
type mediaRetentionStore interface {
	ListOrphanedDocumentIDsOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]int64, error)
	ListOrphanedPhotoIDsOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]int64, error)
	CountFileBlobRefs(ctx context.Context, backend, objectKey string) (int, error)
	DeleteDocumentAndBlobs(ctx context.Context, id int64) ([]domain.FileBlob, error)
	DeletePhotoAndBlobs(ctx context.Context, id int64) ([]domain.FileBlob, error)
}

// DeleteOrphanedOlderThan implements maintenance.OrphanedMediaRetentionStore:
// permanently deletes documents/photos that have had no live reference
// (message/profile-photo/sticker-set, see media_references) since at least
// cutoff, along with their blob(s) -- but only physically removes bytes
// from the backend once confirming no other file_blobs row still needs the
// object, since content-addressed storage means the same bytes can be
// shared across documents/photos.
func (s *Service) DeleteOrphanedOlderThan(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	store, ok := s.media.(mediaRetentionStore)
	if !ok || limit <= 0 {
		return 0, nil
	}
	deleted := 0
	docIDs, err := store.ListOrphanedDocumentIDsOlderThan(ctx, cutoff, limit)
	if err != nil {
		return deleted, fmt.Errorf("list orphaned documents: %w", err)
	}
	for _, id := range docIDs {
		blobs, err := store.DeleteDocumentAndBlobs(ctx, id)
		if err != nil {
			s.log.Warn("delete orphaned document failed", zap.Int64("document_id", id), zap.Error(err))
			continue
		}
		s.deleteOrphanedBlobs(ctx, store, blobs)
		deleted++
	}
	photoIDs, err := store.ListOrphanedPhotoIDsOlderThan(ctx, cutoff, limit)
	if err != nil {
		return deleted, fmt.Errorf("list orphaned photos: %w", err)
	}
	for _, id := range photoIDs {
		blobs, err := store.DeletePhotoAndBlobs(ctx, id)
		if err != nil {
			s.log.Warn("delete orphaned photo failed", zap.Int64("photo_id", id), zap.Error(err))
			continue
		}
		s.deleteOrphanedBlobs(ctx, store, blobs)
		deleted++
	}
	return deleted, nil
}

// deleteOrphanedBlobs removes each blob from its backend once confirming
// (via CountFileBlobRefs) no other file_blobs row still references
// (backend, object_key). Only blobs on the currently active backend are
// physically removed -- a blob left over from a previously active backend
// (deployment switched TELESRV_BLOB_BACKEND at some point; switching back
// isn't supported) is logged and skipped rather than silently dropped,
// since there's no configured client to reach it right now anyway.
func (s *Service) deleteOrphanedBlobs(ctx context.Context, store mediaRetentionStore, blobs []domain.FileBlob) {
	for _, b := range blobs {
		refs, err := store.CountFileBlobRefs(ctx, string(b.Backend), b.ObjectKey)
		if err != nil {
			s.log.Warn("count file blob refs failed", zap.String("object_key", b.ObjectKey), zap.Error(err))
			continue
		}
		if refs > 0 {
			continue
		}
		if string(b.Backend) != s.blobs.Name() {
			s.log.Warn("orphaned blob is on an inactive backend, skipping physical delete",
				zap.String("backend", string(b.Backend)), zap.String("object_key", b.ObjectKey))
			continue
		}
		if err := s.blobs.Delete(ctx, b.ObjectKey); err != nil {
			s.log.Warn("delete orphaned blob failed", zap.String("object_key", b.ObjectKey), zap.Error(err))
		}
	}
}
