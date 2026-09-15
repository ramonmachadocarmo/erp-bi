package application

import (
	"context"
	"log"
	"time"
)

// RunScheduler ticks every interval and runs any budget schedule whose
// next_run_at is due. Mirrors pkg/outbox.Run's ticker-loop shape, the only
// scheduling precedent in this codebase.
func RunScheduler(ctx context.Context, svc *Service, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.runDueSchedules(ctx)
		}
	}
}

func (s *Service) runDueSchedules(ctx context.Context) {
	due, err := s.schedules.Due(ctx, time.Now().UTC())
	if err != nil {
		log.Printf("bi scheduler fetch: %v", err)
		return
	}
	for _, sch := range due {
		if _, err := s.RunSchedule(ctx, sch); err != nil {
			log.Printf("bi scheduler run %s: %v", sch.ID, err)
		}
	}
}
