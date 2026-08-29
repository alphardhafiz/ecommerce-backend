package jobs

import (
	"context"
	"log/slog"
	"time"

	"ecommerce/server/internal/repository"
)

// ExpireOrders runs every interval and expires PENDING orders past their
// 60-minute deadline, returning stock (PRD C.9, F.3). Runs until ctx is
// cancelled; logs but never panics on a failed sweep.
func ExpireOrders(ctx context.Context, orders *repository.OrderRepo, interval time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := orders.ExpirePending(ctx)
			if err != nil {
				log.Error("expire orders sweep failed", "error", err)
				continue
			}
			if n > 0 {
				log.Info("expired overdue orders", "count", n)
			}
		}
	}
}
