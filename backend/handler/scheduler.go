package handler

import (
	"auction/logging"
	"context"
	"time"

	"go.uber.org/zap"
)

const schedulerEvery = 5 * time.Second

// StartLotScheduler запускает аукционы одобренных лотов, когда наступает их время, и доводит
// до статуса finished аукционы с вышедшим сроком (в том числе закончившиеся, пока сервер стоял).
// Работает до отмены ctx. Вызывать один раз при старте, после StartLive.
func (h *Handler) StartLotScheduler(ctx context.Context) {
	lg := h.logger().With(zap.String("component", logging.ComponentLive))
	tick := func() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		started, err := h.q.ActivateDueLots(cctx)
		if err != nil && ctx.Err() == nil {
			lg.Error("failed to start scheduled auctions", zap.Error(err))
		}
		for _, a := range started {
			h.hub.LotCreated(a.ID, a.EndsAt)
			lg.Info("scheduled auction started", zap.Int64("lot_id", a.ID))
		}
		if _, err := h.q.FinalizeExpired(cctx); err != nil && ctx.Err() == nil {
			lg.Error("failed to finalize expired auctions", zap.Error(err))
		}
	}
	go func() {
		t := time.NewTicker(schedulerEvery)
		defer t.Stop()
		for {
			tick()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
