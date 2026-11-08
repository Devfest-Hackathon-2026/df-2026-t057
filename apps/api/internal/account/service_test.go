package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/account"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
)

func TestDeletionFenceGraceAndReceipt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	w := testkit.New(t, now)
	s := account.Service{Runner: w.Runner, Clock: w.Clock, IDs: id.Random{}, Data: w.Store, Enabled: true}
	if _, err := w.Tasks.Create(ctx, "u1", task.CreateRequest{ID: "a", Input: task.Input{Title: "test", Minutes: 30, Deadline: now.Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(ctx, "u1", now.Add(-6*time.Minute)); !errors.Is(err, apperr.Forbidden("account.reauth_required")) {
		t.Fatalf("reauth err=%v", err)
	}
	receipt, err := s.Begin(ctx, "u1", now)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := s.Status(ctx, receipt); err != nil || state != account.Pending {
		t.Fatalf("status=%s err=%v", state, err)
	}
	if _, err := s.Status(ctx, receipt[:63]+"z"); err == nil {
		t.Fatal("wrong receipt accepted")
	}
	err = w.Runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		return tx.Set(store.UserDoc("u1", "profile"), map[string]string{"name": "late"})
	})
	if !errors.Is(err, apperr.Forbidden("account.closed")) {
		t.Fatalf("late write=%v", err)
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ := w.Store.Export(ctx, "u1")
	if len(data["tasks"]) != 1 {
		t.Fatal("cleaned before grace")
	}
	w.Clock.Advance(91 * time.Second)
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ = w.Store.Export(ctx, "u1")
	if len(data["tasks"]) != 0 {
		t.Fatal("user data left")
	}
	if state, err := s.Status(ctx, receipt); err != nil || state != account.Complete {
		t.Fatalf("status=%s err=%v", state, err)
	}
	if err = s.CheckOpen(ctx, "u1"); !errors.Is(err, apperr.Forbidden("account.closed")) {
		t.Fatalf("recreated=%v", err)
	}
	if err = s.CheckOpen(ctx, "u2"); err != nil {
		t.Fatal(err)
	}
}
