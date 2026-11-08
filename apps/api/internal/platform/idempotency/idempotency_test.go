package idempotency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/idempotency"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

var ctx = context.Background()

func setup() (*idempotency.Service, *store.Runner, *clock.Fake) {
	c := clock.NewFake(time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC))
	r := store.NewRunner(memory.NewStore())
	return idempotency.New(r, c, &id.Sequence{}), r, c
}

func mutate(ctx context.Context, r *store.Runner) error {
	return r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		_, err := tx.Touch(ctx, "u", store.ResourceTasks)
		return err
	})
}

func TestBeginReplayReuseInProgress(t *testing.T) {
	s, r, c := setup()
	a, replay, err := s.Begin(ctx, "u", "key-0001", "h1")
	if err != nil || replay != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Begin(ctx, "u", "key-0001", "h1"); !errors.Is(err, apperr.Conflict("idempotency.in_progress")) {
		t.Fatalf("in progress: %v", err)
	}
	if _, _, err := s.Begin(ctx, "u", "key-0001", "h2"); !errors.Is(err, apperr.Conflict("idempotency.key_reused")) {
		t.Fatalf("reused: %v", err)
	}
	if err := mutate(idempotency.WithAttempt(ctx, a), r); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, a, 201, map[string]string{"Etag": `"1"`}, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	_, replay, err = s.Begin(ctx, "u", "key-0001", "h1")
	if err != nil || replay == nil || replay.Status != 201 || string(replay.Body) != "{}" {
		t.Fatalf("replay %+v %v", replay, err)
	}
	c.Advance(idempotency.TTL + time.Second)
	if _, replay, err := s.Begin(ctx, "u", "key-0001", "h2"); err != nil || replay != nil {
		t.Fatalf("expired key: %+v %v", replay, err)
	}
}

func TestMutationAppliedOnlyOnceAfterLeaseTakeover(t *testing.T) {
	s, r, c := setup()
	first, _, _ := s.Begin(ctx, "u", "key-0002", "h")
	if err := mutate(idempotency.WithAttempt(ctx, first), r); err != nil {
		t.Fatal(err)
	}
	// 応答を記録する前に止まった。期限が切れた後の再送は、変更を二度反映しない。
	c.Advance(idempotency.Lease + time.Second)
	second, _, err := s.Begin(ctx, "u", "key-0002", "h")
	if err != nil {
		t.Fatal(err)
	}
	if err := mutate(idempotency.WithAttempt(ctx, second), r); !errors.Is(err, apperr.Conflict("idempotency.in_progress")) {
		t.Fatalf("second apply: %v", err)
	}
	// 何も反映していない失敗は記録を消し、同じキーで再試行できる。
	third, _, _ := s.Begin(ctx, "u", "key-0003", "h")
	if err := s.Complete(ctx, third, 412, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, replay, err := s.Begin(ctx, "u", "key-0003", "h"); err != nil || replay != nil {
		t.Fatalf("retry after unapplied failure: %v", err)
	}
}
