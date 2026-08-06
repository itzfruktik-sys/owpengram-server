package broadcast

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// defaultInterval/defaultBatch match the shipped
// TELESRV_BROADCAST_WORKER_INTERVAL/_BATCH defaults.
const (
	defaultInterval = 3 * time.Second
	defaultBatch    = 50
)

// Worker drains the broadcast delivery outbox.
//
// A broadcast is created together with its recipient snapshot, never with the
// sends themselves: an admin creating a broadcast for every user must not
// wait on however long that takes. Delivery is therefore a separate,
// retrying cycle over durable rows, and this worker is only its cadence.
type Worker struct {
	service  *Service
	logger   *zap.Logger
	interval time.Duration
	batch    int
}

// NewWorker creates the periodic delivery worker. Non-positive
// interval/batch fall back to the shipped defaults.
func NewWorker(service *Service, logger *zap.Logger, interval time.Duration, batch int) *Worker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if interval <= 0 {
		interval = defaultInterval
	}
	if batch <= 0 {
		batch = defaultBatch
	}
	return &Worker{service: service, logger: logger, interval: interval, batch: batch}
}

// Run delivers one batch immediately and then on every tick until ctx is
// done. A not-ready service (missing store/sender) exits immediately with
// one explicit log line instead of ticking forever over a no-op.
func (w *Worker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	if !w.service.Ready() {
		w.logger.Info("broadcast delivery worker disabled: not configured")
		return
	}
	w.runOnce(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	if w == nil || w.service == nil {
		return
	}
	sent, err := w.service.RunSendCycle(ctx, w.batch)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		w.logger.Warn("broadcast delivery cycle failed", zap.Int("sent", sent), zap.Int("batch", w.batch), zap.Error(err))
		return
	}
	if sent > 0 {
		w.logger.Info("broadcast delivery cycle completed", zap.Int("sent", sent), zap.Int("batch", w.batch))
	}
}
