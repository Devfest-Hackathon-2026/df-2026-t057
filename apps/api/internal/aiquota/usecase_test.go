package aiquota

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

func newQuota(limits Limits) (*Quota, *clock.Fake, *memory.Store) {
	mem := memory.NewStore()
	c := clock.NewFake(time.Date(2026, 11, 7, 12, 0, 10, 0, time.UTC))
	return New(store.NewRunner(mem), c, limits), c, mem
}

func TestTakeLimitsUserAndReportsRetryAfter(t *testing.T) {
	q, c, _ := newQuota(Limits{UserPerMinute: 2, UserPerDay: 30, GlobalPerMinute: 30, GlobalPerDay: 300})
	for range 2 {
		if err := q.Take(t.Context(), "a"); err != nil {
			t.Fatal(err)
		}
	}
	err := q.Take(t.Context(), "a")
	var e *apperr.Error
	if !errors.As(err, &e) || e.Kind != apperr.KindRateLimited || e.Code != "ai.rate_limited" || e.RetryAfter != 50*time.Second {
		t.Fatalf("limit: %v", err)
	}
	if err := q.Take(t.Context(), "b"); err != nil {
		t.Fatalf("another user was blocked: %v", err)
	}
	c.Advance(50 * time.Second)
	if err := q.Take(t.Context(), "a"); err != nil {
		t.Fatalf("next minute: %v", err)
	}
}

func TestTakeStoresHashedUserDocument(t *testing.T) {
	q, _, mem := newQuota(Limits{1, 1, 1, 1})
	if err := q.Take(t.Context(), "private-uid"); err != nil {
		t.Fatal(err)
	}
	err := store.NewRunner(mem).View(t.Context(), func(ctx context.Context, tx *store.Tx) error {
		u, found, err := store.Get[Usage](ctx, tx, userRef("private-uid"))
		if err != nil || !found || u.DayCalls != 1 {
			t.Fatalf("user usage: %+v %v %v", u, found, err)
		}
		if userRef("private-uid").ID == "user-private-uid" || len(userRef("x").ID) != len("user-")+64 {
			t.Fatal("uid is not hashed")
		}
		g, found, err := store.Get[Usage](ctx, tx, store.GlobalDoc("aiUsage", "global"))
		if err != nil || !found || g.MinuteCalls != 1 {
			t.Fatalf("global usage: %+v", g)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTakeConcurrentUsersShareGlobalLimit(t *testing.T) {
	q, _, _ := newQuota(Limits{UserPerMinute: 5, UserPerDay: 30, GlobalPerMinute: 7, GlobalPerDay: 7})
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			err := q.Take(t.Context(), strconv.Itoa(i))
			if err == nil {
				accepted.Add(1)
				return
			}
			if apperr.As(err).Code != "ai.rate_limited" {
				t.Errorf("unexpected failure: %v", err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 7 {
		t.Fatalf("global limit admitted %d calls", accepted.Load())
	}
}

func TestTakeUnavailable(t *testing.T) {
	q, _, _ := newQuota(Limits{1, 1, 0, 1})
	if err := q.Take(t.Context(), "a"); apperr.As(err).Code != "ai.unavailable" {
		t.Fatalf("invalid limits: %v", err)
	}
	q, _, mem := newQuota(Limits{5, 5, 5, 5})
	if err := q.Take(t.Context(), ""); apperr.As(err).Code != "ai.unavailable" {
		t.Fatalf("missing owner: %v", err)
	}
	mem.FailNextCommit(errors.New("private-store-detail"))
	err := q.Take(t.Context(), "a")
	if e := apperr.As(err); e.Kind != apperr.KindUnavailable || e.Code != "ai.unavailable" {
		t.Fatalf("store failure: %v", err)
	}
}
