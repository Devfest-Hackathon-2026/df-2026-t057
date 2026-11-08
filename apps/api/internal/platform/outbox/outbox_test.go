package outbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

type dispatcher struct {
	mu   sync.Mutex
	jobs []string
}

func (d *dispatcher) Dispatch(_ context.Context, _, jobID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.jobs = append(d.jobs, jobID)
	return nil
}

func TestEnqueueDispatchesAfterCommitAndGivesUp(t *testing.T) {
	ctx := context.Background()
	r := store.NewRunner(memory.NewStore())
	d := &dispatcher{}
	o := outbox.New(r, d, clock.NewFake(time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)), &id.Sequence{Prefix: "j"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	calls := 0
	o.Handle(outbox.KindSync, func(context.Context, outbox.Job) error {
		calls++
		return apperr.External("external.failed", errors.New("down"))
	})
	var job outbox.Job
	boom := errors.New("rollback")
	_ = r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := o.Enqueue(ctx, tx, "u", outbox.KindSync, ""); err != nil {
			return err
		}
		return boom
	})
	if len(d.jobs) != 0 {
		t.Fatal("dispatched a rolled back job")
	}
	err := r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		job, err = o.Enqueue(ctx, tx, "u", outbox.KindSync, "")
		return err
	})
	if err != nil || len(d.jobs) != 1 {
		t.Fatalf("dispatch %v %v", d.jobs, err)
	}
	for i := 1; i < outbox.MaxAttempts; i++ {
		if err := o.Run(ctx, "u", job.ID); err == nil {
			t.Fatalf("attempt %d should ask for a retry", i)
		}
	}
	if err := o.Run(ctx, "u", job.ID); err != nil {
		t.Fatalf("last attempt: %v", err)
	}
	_ = o.Run(ctx, "u", job.ID)
	var pending, failed int
	_ = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		pending, failed, err = outbox.Counts(ctx, tx, "u")
		return err
	})
	if calls != outbox.MaxAttempts || pending != 0 || failed != 1 {
		t.Fatalf("calls=%d pending=%d failed=%d", calls, pending, failed)
	}
}
