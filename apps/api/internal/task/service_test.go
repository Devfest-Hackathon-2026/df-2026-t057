package task_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
)

const uid = "u1"

var ctx = context.Background()
var noon = time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST)

func input(title string, minutes int, deadline time.Time) task.Input {
	return task.Input{Title: title, Minutes: minutes, Deadline: deadline}
}

func TestRegistrationGrantsOnceForSameID(t *testing.T) {
	w := testkit.New(t, noon)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "card-1", Input: input("a", 30, noon.Add(time.Hour*3))})
		}()
	}
	wg.Wait()
	l, _ := w.Tasks.List(ctx, uid)
	if len(l.Items) != 1 || w.Account(t, uid).Balance != 900 {
		t.Fatalf("tasks=%d balance=%d", len(l.Items), w.Account(t, uid).Balance)
	}
	_, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "card-1", Input: input("a", 30, noon.Add(time.Hour*3))})
	if !errors.Is(err, apperr.Conflict("task.duplicate")) {
		t.Fatalf("err = %v", err)
	}
	// 消した課題のカードを押し直しても、報酬は二度付かない。
	if err := w.Tasks.Delete(ctx, uid, "card-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "card-1", Input: input("a", 30, noon.Add(time.Hour*3))}); err != nil {
		t.Fatal(err)
	}
	if b := w.Account(t, uid).Balance; b != 900 {
		t.Fatalf("balance = %d", b)
	}
	w.CheckProjection(t, uid)
}

func TestAccountsAreIsolated(t *testing.T) {
	w := testkit.New(t, noon)
	tk := w.CreateTask(t, uid, "a", 30, noon.Add(3*time.Hour))
	if _, err := w.Work.Start(ctx, "u2", tk.ID); !errors.Is(err, apperr.NotFound("task.not_found")) {
		t.Fatalf("start = %v", err)
	}
	if _, err := w.Work.Complete(ctx, "u2", tk.ID); !errors.Is(err, apperr.NotFound("task.not_found")) {
		t.Fatalf("complete = %v", err)
	}
	if _, err := w.Tasks.RescheduleDraft(ctx, "u2", tk.ID, task.RescheduleRequest{Kind: task.DraftSnooze}); !errors.Is(err, apperr.NotFound("task.not_found")) {
		t.Fatalf("draft = %v", err)
	}
	if a := w.Account(t, "u2"); a.Balance != 600 {
		t.Fatalf("other balance = %d", a.Balance)
	}
}

func TestStaleIfMatchOnCreate(t *testing.T) {
	w := testkit.New(t, noon)
	w.CreateTask(t, uid, "a", 30, noon.Add(3*time.Hour))
	stale := int64(0)
	_, err := w.Tasks.Create(ctx, uid, task.CreateRequest{Input: input("b", 30, noon.Add(3*time.Hour)), IfMatch: &stale})
	if !errors.Is(err, apperr.PreconditionFailed("precondition.failed")) {
		t.Fatalf("err = %v", err)
	}
}

func TestNextSnoozeInvalidatedBySettings(t *testing.T) {
	w := testkit.New(t, noon)
	w.CreateTask(t, uid, "a", 30, noon.Add(3*time.Hour))
	l, _ := w.Tasks.List(ctx, uid)
	if l.NextSnooze == nil {
		t.Fatal("no next snooze")
	}
	if _, _, err := w.Profiles.UpdateSettings(ctx, uid, profile.Settings{SnoozeMinutes: 15, Mode: profile.ModeSimple}, 0); err != nil {
		t.Fatal(err)
	}
	changes := []task.ChangeInput{{TaskID: l.NextSnooze.Changes[0].TaskID, After: l.NextSnooze.Changes[0].After}}
	if _, err := w.Tasks.Commit(ctx, uid, changes, l.NextSnooze.BaseRevision); !errors.Is(err, apperr.PreconditionFailed("precondition.failed")) {
		t.Fatalf("stale commit = %v", err)
	}
	fresh, _ := w.Tasks.List(ctx, uid)
	changes = []task.ChangeInput{{TaskID: fresh.NextSnooze.Changes[0].TaskID, After: fresh.NextSnooze.Changes[0].After}}
	after, err := w.Tasks.Commit(ctx, uid, changes, fresh.Revision)
	if err != nil || !after.Items[0].Plan.Start.Equal(changes[0].After.Start) {
		t.Fatalf("commit %v", err)
	}
	if b := w.Account(t, uid).Balance; b != 900 {
		t.Fatalf("balance = %d", b)
	}
	// 設定の見た目だけを変えても、計画の版は上がらない。
	if _, _, err := w.Profiles.UpdateSettings(ctx, uid, profile.Settings{SnoozeMinutes: 15, Mode: profile.ModeMahjong}, 1); err != nil {
		t.Fatal(err)
	}
	if l2, _ := w.Tasks.List(ctx, uid); l2.Revision != after.Revision {
		t.Fatal("cosmetic settings bumped the plan revision")
	}
}

func TestEditSavesContentAndRecomputesTimetable(t *testing.T) {
	w := testkit.New(t, noon)
	tk := w.CreateTask(t, uid, "old", 30, noon.Add(5*time.Hour))
	l, _ := w.Tasks.List(ctx, uid)
	in := task.Input{Title: "new", Notes: "メモ", Why: " 理由 ", Minutes: 15, Deadline: noon.Add(4 * time.Hour), URL: "https://example.com"}
	d, err := w.Tasks.PreviewEdit(ctx, uid, tk.ID, in)
	if err != nil || d.Kind != task.DraftEdit {
		t.Fatalf("preview %v", err)
	}
	if l2, _ := w.Tasks.List(ctx, uid); l2.Revision != l.Revision {
		t.Fatal("preview saved")
	}
	c, err := w.Tasks.Update(ctx, uid, tk.ID, in, l.Revision)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Task
	if got.Title != "new" || got.Why != "理由" || got.Minutes != 15 || !got.CreatedAt.Equal(tk.CreatedAt) || len(got.Plan.Steps) != 1 || got.Plan.Steps[0].Title != "new" {
		t.Fatalf("task %+v", got)
	}
	if _, err := w.Tasks.Update(ctx, uid, tk.ID, in, l.Revision); !errors.Is(err, apperr.PreconditionFailed("precondition.failed")) {
		t.Fatalf("stale retry = %v", err)
	}
	if b := w.Account(t, uid).Balance; b != 900 {
		t.Fatalf("edit granted: %d", b)
	}
	if _, err := w.Work.Start(ctx, uid, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tasks.PreviewEdit(ctx, uid, tk.ID, in); !errors.Is(err, apperr.Conflict("task.not_editable")) {
		t.Fatalf("active edit = %v", err)
	}
}

func TestAILimitFallsBackToBackwardPlan(t *testing.T) {
	w := testkit.New(t, noon)
	w.Planner.Err = apperr.RateLimited("ai.rate_limited", time.Minute)
	tk := w.CreateTask(t, uid, "a", 30, noon.Add(3*time.Hour))
	if tk.Plan.Reason != task.ReasonAILimitFallback {
		t.Fatalf("reason %s", tk.Plan.Reason)
	}
	w.Planner.Err = nil
	w.Planner.Result = task.DetailResult{Steps: []task.StepSpec{{Title: "構成", Minutes: 10}, {Title: "書く", Minutes: 20}}, Note: "メモ"}
	tk = w.CreateTask(t, uid, "b", 30, noon.Add(6*time.Hour))
	if tk.Plan.Reason != task.ReasonAIDetailed || len(tk.Plan.Steps) != 2 || tk.Plan.Note != "メモ" {
		t.Fatalf("plan %+v", tk.Plan)
	}
}

func TestStartCompleteFlow(t *testing.T) {
	w := testkit.New(t, noon)
	a := w.CreateTask(t, uid, "a", 30, noon.Add(3*time.Hour))
	b := w.CreateTask(t, uid, "b", 30, noon.Add(6*time.Hour))
	s1, err := w.Work.Start(ctx, uid, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(time.Minute)
	s2, err := w.Work.Start(ctx, uid, a.ID)
	if err != nil || s2.Session.ID != s1.Session.ID || !s2.Session.StartedAt.Equal(s1.Session.StartedAt) {
		t.Fatalf("restart %v", err)
	}
	if _, err := w.Work.Start(ctx, uid, b.ID); !errors.Is(err, apperr.Conflict("task.another_active")) {
		t.Fatalf("another = %v", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = w.Work.Complete(ctx, uid, a.ID) }()
	}
	wg.Wait()
	kinds := map[string]int{}
	for _, e := range w.Entries(t, uid) {
		kinds[string(e.Kind)]++
	}
	if kinds["work"] != 1 || kinds["registration"] != 2 {
		t.Fatalf("ledger %v", kinds)
	}
	if _, err := w.Work.Start(ctx, uid, a.ID); !errors.Is(err, apperr.Conflict("task.already_completed")) {
		t.Fatalf("start completed = %v", err)
	}
	if snap, _ := w.Work.Get(ctx, uid); snap != nil {
		t.Fatal("session left")
	}
	w.CheckProjection(t, uid)
}

func TestDeleteClearsTimerAndKeepsReward(t *testing.T) {
	w := testkit.New(t, noon)
	a := w.CreateTask(t, uid, "a", 30, noon.Add(3*time.Hour))
	if _, err := w.Work.Start(ctx, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.Tasks.Delete(ctx, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if snap, _ := w.Work.Get(ctx, uid); snap != nil {
		t.Fatal("timer left")
	}
	if b := w.Account(t, uid).Balance; b != 900 {
		t.Fatalf("balance %d", b)
	}
}

func TestFailedCommitRollsBack(t *testing.T) {
	w := testkit.New(t, noon)
	w.Store.FailNextCommit(errors.New("boom"))
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{Input: input("a", 30, noon.Add(3*time.Hour))}); err == nil {
		t.Fatal("expected failure")
	}
	l, _ := w.Tasks.List(ctx, uid)
	if len(l.Items) != 0 || l.Revision != 0 {
		t.Fatalf("partial write %+v", l)
	}
}

func TestSamplesOnlyWhenEmpty(t *testing.T) {
	w := testkit.New(t, noon)
	l, err := w.Tasks.AddSamples(ctx, uid)
	if err != nil || len(l.Items) != 3 {
		t.Fatalf("%v", err)
	}
	if b := w.Account(t, uid).Balance; b != 1500 {
		t.Fatalf("balance %d", b)
	}
	if _, err := w.Tasks.AddSamples(ctx, uid); !errors.Is(err, apperr.Conflict("demo.tasks_exist")) {
		t.Fatalf("err = %v", err)
	}
}
