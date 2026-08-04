package maintenance

import (
	"context"
	"time"
)

// OrphanedMediaRetentionStore deletes documents/photos that have been
// orphaned (no live message/profile-photo/sticker-set reference remains,
// tracked via media_references + orphaned_at) for at least the configured
// age, along with their underlying blob once no other file_blobs row on
// its backend still needs it. Never touches media that still has a live
// reference, regardless of age.
type OrphanedMediaRetentionStore interface {
	DeleteOrphanedOlderThan(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

// WithOrphanedMediaRetention enables the storage retention sweep. maxAge is
// how long a document/photo must have been orphaned before it's actually
// deleted (not how old the media itself is) -- <=0 leaves the sweep
// disabled even if a store is provided, matching
// TELESRV_STORAGE_RETENTION_ENABLE=false being the safe default.
func (w *RetentionWorker) WithOrphanedMediaRetention(store OrphanedMediaRetentionStore, maxAge time.Duration) *RetentionWorker {
	w.orphanedMedia = store
	w.orphanedMediaMaxAge = maxAge
	return w
}
