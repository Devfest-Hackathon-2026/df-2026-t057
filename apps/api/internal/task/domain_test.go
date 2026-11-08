package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

var now = time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)

func pending(id string, minutes int, deadline time.Time) Task {
	t := NewTask(id, Input{Title: id, Minutes: minutes, Deadline: deadline}, now)
	t.Plan = BackwardPlan(t, ReasonBackward)
	return t
}

func TestValidateInput(t *testing.T) {
	ok := Input{Title: "レポート", Minutes: 30, Deadline: now.Add(time.Hour)}
	cases := []struct {
		name  string
		edit  func(*Input)
		field string
	}{
		{"blank title", func(in *Input) { in.Title = "  " }, "task.title_length"},
		{"long title", func(in *Input) { in.Title = strings.Repeat("あ", 201) }, "task.title_length"},
		{"minutes", func(in *Input) { in.Minutes = 721 }, "task.minutes_range"},
		{"past", func(in *Input) { in.Deadline = now.Add(-time.Second) }, "task.deadline_past"},
		{"why", func(in *Input) { in.Why = strings.Repeat("a", 201) }, "task.why_length"},
		{"url", func(in *Input) { in.URL = "javascript:alert(1)" }, "task.url_scheme"},
	}
	if err := ValidateInput(ok, now); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		in := ok
		c.edit(&in)
		err := ValidateInput(in, now)
		var e *apperr.Error
		if !errors.As(err, &e) || e.Fields[0].Code != c.field {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

func TestReplanBackward(t *testing.T) {
	due := now.Add(90 * time.Minute)
	tasks := []Task{pending("a", 30, due), pending("b", 30, due)}
	Replan(tasks)
	if !tasks[0].Plan.Start.Equal(now.Add(30*time.Minute)) || !tasks[1].Plan.End.Equal(due) {
		t.Fatalf("a=%v b=%v", tasks[0].Plan, tasks[1].Plan)
	}
	// 後の締切の課題は、前の締切の課題を遅らせない。
	tasks = []Task{pending("early", 30, now.Add(time.Hour)), pending("late", 30, now.Add(5*time.Hour))}
	Replan(tasks)
	if !tasks[0].Plan.End.Equal(now.Add(time.Hour)) {
		t.Fatalf("early %v", tasks[0].Plan)
	}
}

func TestReplanKeepsConcreteSteps(t *testing.T) {
	task := NewTask("a", Input{Title: "a", Minutes: 30, Deadline: now.Add(2 * time.Hour)}, now)
	plan, ok := DetailedPlan(task, []StepSpec{{"構成", 10}, {"本文", 20}}, "")
	if !ok {
		t.Fatal("detail")
	}
	task.Plan = plan
	tasks := []Task{task, pending("b", 30, now.Add(2*time.Hour))}
	Replan(tasks)
	p := tasks[0].Plan
	if len(p.Steps) != 2 || p.Steps[0].End.Sub(p.Steps[0].Start) != 10*time.Minute || p.Reason != ReasonAIDetailed {
		t.Fatalf("plan %+v", p)
	}
	if _, ok := DetailedPlan(task, []StepSpec{{"x", 20}}, ""); ok {
		t.Fatal("wrong total accepted")
	}
}

func TestMakeDraftCompressesWithinDeadline(t *testing.T) {
	due := now.Add(90 * time.Minute)
	tasks := []Task{pending("a", 30, due), pending("b", 30, due), pending("c", 15, due)}
	Replan(tasks)
	before := tasks[0].Plan
	d, err := MakeDraft(tasks, "a", DraftSnooze, now.Add(30*time.Minute), now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Shortened || d.Changes[0].PlannedSeconds >= 1800 || !d.Changes[2].After.End.Equal(due) && d.Changes[2].After.End.After(due) {
		t.Fatalf("draft %+v", d)
	}
	inputs := make([]ChangeInput, len(d.Changes))
	for i, c := range d.Changes {
		inputs[i] = ChangeInput{TaskID: c.TaskID, After: c.After}
	}
	if err := ValidateChanges(inputs, tasks, now); err != nil {
		t.Fatal(err)
	}
	if !tasks[0].Plan.Start.Equal(before.Start) {
		t.Fatal("MakeDraft mutated the plan")
	}
	if _, err := MakeDraft(tasks, "a", DraftSnooze, due.Add(time.Minute), now, 1); !errors.Is(err, apperr.Conflict("plan.deadline_passed")) {
		t.Fatalf("after deadline: %v", err)
	}
	again, _ := MakeDraft(tasks, "a", DraftSnooze, now.Add(30*time.Minute), now, 1)
	if again.ID != d.ID {
		t.Fatal("draft id should be stable")
	}
}

func TestValidateChangesRejects(t *testing.T) {
	due := now.Add(5 * time.Hour)
	tasks := []Task{pending("a", 30, due), pending("b", 30, now.Add(3*time.Hour)), pending("c", 30, due)}
	Replan(tasks)
	plan := func(id string, start time.Time, minutes int) ChangeInput {
		t, _ := Find(tasks, id)
		return ChangeInput{TaskID: id, After: PlanAt(*t, start, start.Add(time.Duration(minutes)*time.Minute), ReasonManual)}
	}
	codeOf := func(err error) string {
		var e *apperr.Error
		if errors.As(err, &e) && len(e.Fields) > 0 {
			return e.Fields[0].Code
		}
		return ""
	}
	overlap := []ChangeInput{plan("a", now.Add(time.Hour), 30), plan("c", now.Add(70*time.Minute), 30)}
	if got := codeOf(ValidateChanges(overlap, tasks, now)); got != "plan.overlap" {
		t.Fatalf("overlap: %s", got)
	}
	steps := plan("a", now.Add(time.Hour), 30)
	steps.After.Steps[0].End = steps.After.Steps[0].End.Add(time.Minute)
	if got := codeOf(ValidateChanges([]ChangeInput{steps}, tasks, now)); got != "plan.steps_invalid" {
		t.Fatalf("steps: %s", got)
	}
	b, _ := Find(tasks, "b")
	other := plan("a", b.Plan.Start.Add(10*time.Minute), 30)
	if got := codeOf(ValidateChanges([]ChangeInput{other}, tasks, now)); got != "plan.overlaps_other" {
		t.Fatalf("other: %s", got)
	}
	late := plan("b", now.Add(170*time.Minute), 30)
	if got := codeOf(ValidateChanges([]ChangeInput{late}, tasks, now)); got != "plan.exceeds_deadline" {
		t.Fatalf("deadline: %s", got)
	}
}

func TestNextSnoozeKeepsConfirmedOrder(t *testing.T) {
	due := now.Add(5 * time.Hour)
	tasks := []Task{pending("a", 30, due), pending("b", 30, due)}
	Replan(tasks)
	tasks[0].Plan, tasks[1].Plan = PlanAt(tasks[0], tasks[1].Plan.Start, tasks[1].Plan.End, ReasonManual), PlanAt(tasks[1], tasks[0].Plan.Start, tasks[0].Plan.End, ReasonManual)
	d := NextSnooze(tasks, 10, now, 3)
	if d == nil || d.Changes[0].TaskID != "b" || d.Changes[1].TaskID != "a" || d.BaseRevision != 3 {
		t.Fatalf("draft %+v", d)
	}
}

func TestEditDraftRejectsActiveTask(t *testing.T) {
	tasks := []Task{pending("a", 30, now.Add(time.Hour))}
	tasks[0].Status = StatusActive
	if _, _, err := EditDraft(tasks, "a", Input{Title: "x", Minutes: 10, Deadline: now.Add(time.Hour)}, now, 1); !errors.Is(err, apperr.Conflict("task.not_editable")) {
		t.Fatalf("err = %v", err)
	}
	tasks[0].Status = StatusPending
	if _, _, err := EditDraft(tasks, "a", Input{Title: "x", Minutes: 720, Deadline: now.Add(2 * time.Minute)}, now, 1); err == nil {
		t.Fatal("impossible timetable accepted")
	}
}
