package companion

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/worksession"
)

type noCalendar struct{}

func (noCalendar) TaskContentChanged(context.Context, *store.Tx, string, string) error { return nil }
func (noCalendar) PlansChanged(context.Context, *store.Tx, string) error               { return nil }
func (noCalendar) TaskRemoved(context.Context, *store.Tx, string, string) error        { return nil }
func (noCalendar) LinkedTasks(context.Context, *store.Tx, string) (map[string]bool, error) {
	return nil, nil
}
func (noCalendar) ProfileFormatChanged(context.Context, *store.Tx, string) error { return nil }
func (noCalendar) SleepChanged(context.Context, *store.Tx, string) error         { return nil }
func (noCalendar) SleepCleared(context.Context, *store.Tx, string) error         { return nil }
func (noCalendar) SleepPending(context.Context, *store.Tx, string) (bool, error) {
	return false, nil
}

type noPlanner struct{}

func (noPlanner) Detail(context.Context, string, task.DetailRequest) (task.DetailResult, error) {
	return task.DetailResult{}, apperr.Unavailable("ai.unavailable")
}
func (noPlanner) Extract(context.Context, string, string, time.Time, *time.Location) ([]task.Input, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}
func (noPlanner) Adjust(context.Context, string, task.AdjustRequest) ([]task.ChangeInput, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}

type fakeGenerator struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
	last  LineRequest
}

func (g *fakeGenerator) Line(_ context.Context, _ string, req LineRequest) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	g.last = req
	return g.text, g.err
}

type fakeOpens struct{}

func (fakeOpens) AppOpenCounts(context.Context, *store.Tx, string, time.Time) (int, int, bool, error) {
	return 2, 5, true, nil
}

type world struct {
	runner *store.Runner
	clock  *clock.Fake
	tasks  *task.Service
}

func newWorld(t *testing.T, gen Generator) (*world, *Service) {
	t.Helper()
	runner := store.NewRunner(memory.NewStore())
	c := clock.NewFake(time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC))
	profiles := profile.New(runner, c, noCalendar{})
	wallets := wallet.New(runner, c, profiles, sleep.NewNights(profiles), worksession.Sessions{})
	sleeps := sleep.New(runner, c, profiles, wallets, noCalendar{})
	tasks := task.New(runner, c, &id.Sequence{Prefix: "t"}, task.Deps{
		Planner: noPlanner{}, Calendar: noCalendar{}, Sessions: worksession.Sessions{}, Profiles: profiles, Rewards: wallets,
	})
	svc := New(runner, c, Deps{Tasks: tasks, Profiles: profiles, Wallet: wallets, Sleep: sleeps, AppOpens: fakeOpens{}, Generator: gen})
	return &world{runner: runner, clock: c, tasks: tasks}, svc
}

func (w *world) create(t *testing.T, uid string, in task.Input) task.Task {
	t.Helper()
	if in.Deadline.IsZero() {
		in.Deadline = w.clock.Now().Add(4 * time.Hour)
	}
	if in.Minutes == 0 {
		in.Minutes = 30
	}
	created, err := w.tasks.Create(t.Context(), uid, task.CreateRequest{Input: in})
	if err != nil {
		t.Fatal(err)
	}
	return created.Task
}

func (w *world) complete(t *testing.T, uid, taskID string) {
	t.Helper()
	err := w.runner.Update(t.Context(), func(ctx context.Context, tx *store.Tx) error {
		_, _, err := w.tasks.MarkCompleted(ctx, tx, uid, taskID, w.clock.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLineValidatesMomentAndTask(t *testing.T) {
	_, svc := newWorld(t, nil)
	_, err := svc.Line(t.Context(), "u", Moment("lecture"), "")
	if e := apperr.As(err); e.Code != "request.invalid" || len(e.Fields) != 1 || e.Fields[0].Field != "moment" {
		t.Fatalf("unknown moment: %v", err)
	}
	if _, err := svc.Line(t.Context(), "u", MomentNotice, "missing"); apperr.As(err).Code != "task.not_found" {
		t.Fatalf("unknown task: %v", err)
	}
}

func TestLineWithoutAIReturnsNoText(t *testing.T) {
	w, svc := newWorld(t, nil)
	tk := w.create(t, "u", task.Input{Title: "レポート", Why: "発表で自信を持ちたい"})
	line, err := svc.Line(t.Context(), "u", MomentNotice, tk.ID)
	if err != nil || line.Generated || line.Text != "" {
		t.Fatalf("demo line: %+v %v", line, err)
	}
}

func TestLineIsGeneratedCachedAndSentWithProgress(t *testing.T) {
	gen := &fakeGenerator{text: "「よし、まず資料を開こう。」"}
	w, svc := newWorld(t, gen)
	done := w.create(t, "u", task.Input{Title: "演習"})
	w.complete(t, "u", done.ID)
	tk := w.create(t, "u", task.Input{Title: "レポート", Why: "単位を落とさない"})

	line, err := svc.Line(t.Context(), "u", MomentNotice, tk.ID)
	if err != nil || !line.Generated || line.Text != "よし、まず資料を開こう。" {
		t.Fatalf("generated: %+v %v", line, err)
	}
	req := gen.last
	if req.Moment != MomentNotice || req.Task == nil || req.Task.Why != "単位を落とさない" || req.Progress.StreakDays != 1 ||
		req.Progress.CompletedTotal != 1 || req.Progress.RecentCompleted[0] != "演習" || req.Progress.YouTubeAppOpens == nil ||
		req.Progress.ViewingMinutesLeft <= 0 {
		t.Fatalf("request: %+v", req)
	}

	gen.text = "別の一言"
	if again, _ := svc.Line(t.Context(), "u", MomentNotice, tk.ID); again.Text != line.Text || gen.calls != 1 {
		t.Fatalf("the same moment was generated again: %+v (%d calls)", again, gen.calls)
	}
	if other, _ := svc.Line(t.Context(), "u", MomentComplete, ""); other.Text != "別の一言" || gen.calls != 2 {
		t.Fatalf("another moment reused the line: %+v", other)
	}
	if other, _ := svc.Line(t.Context(), "v", MomentComplete, ""); !other.Generated || gen.calls != 3 {
		t.Fatalf("another user reused the line: %+v", other)
	}
	w.clock.Advance(LineReuse)
	if fresh, _ := svc.Line(t.Context(), "u", MomentNotice, tk.ID); fresh.Text != "別の一言" || gen.calls != 4 {
		t.Fatalf("expired line was reused: %+v", fresh)
	}
}

func TestLineFallsBackOnUnsafeOutputOrAIFailure(t *testing.T) {
	gen := &fakeGenerator{text: "それは依存症ですね"}
	_, svc := newWorld(t, gen)
	if line, err := svc.Line(t.Context(), "u", MomentBalanceZero, ""); err != nil || line.Generated || line.Text != "" {
		t.Fatalf("banned wording was shown: %+v %v", line, err)
	}
	if _, _ = svc.Line(t.Context(), "u", MomentBalanceZero, ""); gen.calls != 2 {
		t.Fatal("a rejected line was cached")
	}
	for _, failure := range []error{
		apperr.RateLimited("ai.rate_limited", time.Minute),
		apperr.External("ai.failed", errors.New("private-upstream-detail")),
		apperr.Unavailable("ai.unavailable"),
	} {
		gen.err = failure
		if line, err := svc.Line(t.Context(), "u", MomentMorning, ""); err != nil || line.Generated || line.Text != "" {
			t.Fatalf("%v: %+v %v", failure, line, err)
		}
	}
}
