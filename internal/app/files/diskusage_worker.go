package files

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// SumFileBlobBytesStore is the minimal store dependency DiskUsageWorker
// needs to refresh an S3BudgetSpaceGuard.
type SumFileBlobBytesStore interface {
	SumFileBlobBytes(ctx context.Context) (int64, error)
}

// DiskUsageWorker periodically refreshes one SpaceGuard's cached usage
// snapshot, so the upload path never pays a statfs syscall / SUM(size)
// query per chunk.
type DiskUsageWorker struct {
	interval time.Duration
	log      *zap.Logger
	refresh  func(ctx context.Context) error
}

// NewLocalDiskUsageWorker refreshes a LocalDiskSpaceGuard from real OS free
// disk bytes under root (the blob backend's storage directory).
func NewLocalDiskUsageWorker(guard *LocalDiskSpaceGuard, root string, interval time.Duration, log *zap.Logger) *DiskUsageWorker {
	return newDiskUsageWorker(interval, log, func(context.Context) error {
		free, total, err := localDiskFreeBytes(root)
		if err != nil {
			return err
		}
		guard.setFree(free, total)
		return nil
	})
}

// NewS3DiskUsageWorker refreshes an S3BudgetSpaceGuard from the tracked
// file_blobs byte total.
func NewS3DiskUsageWorker(guard *S3BudgetSpaceGuard, media SumFileBlobBytesStore, interval time.Duration, log *zap.Logger) *DiskUsageWorker {
	return newDiskUsageWorker(interval, log, func(ctx context.Context) error {
		used, err := media.SumFileBlobBytes(ctx)
		if err != nil {
			return err
		}
		guard.setUsed(used)
		return nil
	})
}

func newDiskUsageWorker(interval time.Duration, log *zap.Logger, refresh func(context.Context) error) *DiskUsageWorker {
	if interval <= 0 {
		interval = time.Minute
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &DiskUsageWorker{interval: interval, log: log, refresh: refresh}
}

// Run refreshes once immediately (so the guard isn't stuck "not ready" for
// a full interval after startup), then on every tick until ctx is done.
func (w *DiskUsageWorker) Run(ctx context.Context) {
	w.refreshOnce(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.refreshOnce(ctx)
		}
	}
}

func (w *DiskUsageWorker) refreshOnce(ctx context.Context) {
	if err := w.refresh(ctx); err != nil {
		w.log.Warn("refresh storage usage snapshot failed", zap.Error(err))
	}
}
