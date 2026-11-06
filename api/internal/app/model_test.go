package app

import (
	"errors"
	"testing"
	"time"
)

func fixture() (State, time.Time) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	s := NewState(now)
	s.Tasks = []Task{
		{ID: "a", Title: "レポート", Minutes: 30, Deadline: now.Add(90 * time.Minute), Status: "pending"},
		{ID: "b", Title: "見直し", Minutes: 30, Deadline: now.Add(90 * time.Minute), Status: "pending"},
	}
	s.replan()
	return s, now
}

func TestBackwardsPlanKeepsDeadlinesAndSteps(t *testing.T) {
	s, now := fixture()
	if !s.Tasks[0].Plan.Start.Equal(now.Add(30*time.Minute)) || !s.Tasks[1].Plan.End.Equal(s.Tasks[1].Deadline) {
		t.Fatal("schedule must be placed backwards from the deadlines")
	}
	s.Tasks[1].Deadline = now.Add(150 * time.Minute)
	s.replan()
	if !s.Tasks[0].Plan.End.Equal(now.Add(90 * time.Minute)) {
		t.Fatal("later deadlines must not delay an earlier deadline")
	}
	s.Tasks[0].Plan.Steps = []Step{{Title: "構成", Start: s.Tasks[0].Plan.Start, End: s.Tasks[0].Plan.Start.Add(10 * time.Minute)}, {Title: "本文", Start: s.Tasks[0].Plan.Start.Add(10 * time.Minute), End: s.Tasks[0].Plan.End}}
	s.replan()
	if len(s.Tasks[0].Plan.Steps) != 2 || s.Tasks[0].Plan.Steps[0].End.Sub(s.Tasks[0].Plan.Steps[0].Start) != 10*time.Minute {
		t.Fatal("replanning lost concrete steps")
	}
}

func TestSnoozeCompressesAndDoesNotCommit(t *testing.T) {
	s, now := fixture()
	original := s.Tasks[0].Plan.Start
	draft, err := MakeDraft(s, "a", now.Add(40*time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Tasks[0].Plan.Start.Equal(original) {
		t.Fatal("preview mutated the confirmed schedule")
	}
	if draft.Changes[0].PlannedMinutes >= 30 {
		t.Fatal("proposal must show reduced work time when the deadline cannot move")
	}
	if err = ValidateChanges(draft.Changes, s, now); err != nil {
		t.Fatal(err)
	}
	if draft.Changes[1].After.End.After(s.Tasks[1].Deadline) {
		t.Fatal("snooze missed the deadline")
	}
	if _, err = MakeDraft(s, "a", now.Add(100*time.Minute), now); err == nil {
		t.Fatal("expired deadline must not yield a feasible plan")
	}
}

func TestDraftRejectsOverlapAndInvalidSteps(t *testing.T) {
	s, now := fixture()
	d, _ := MakeDraft(s, "a", now.Add(time.Minute), now)
	d.Changes[1].After = d.Changes[0].After
	if ValidateChanges(d.Changes, s, now) == nil {
		t.Fatal("overlap accepted")
	}
	d, _ = MakeDraft(s, "a", now.Add(time.Minute), now)
	d.Changes[0].After.Steps[0].End = d.Changes[0].After.End.Add(time.Minute)
	if ValidateChanges(d.Changes, s, now) == nil {
		t.Fatal("step outside plan accepted")
	}
	d, _ = MakeDraft(s, "b", s.Tasks[0].Plan.Start, now)
	if ValidateChanges(d.Changes, s, now) == nil {
		t.Fatal("overlap with unchanged task accepted")
	}
}

func TestRewardOnceAcrossRetriesAndEdits(t *testing.T) {
	s, now := fixture()
	for range 10 {
		s.grant("registration", "a", 600, now)
		s.grant("completion", "a", 1200, now)
	}
	if s.BalanceSeconds != 5400 || len(s.Ledger) != 3 {
		t.Fatalf("duplicate rewards: %+v", s.Ledger)
	}
}

func TestSnoozeKeepsTheManuallyConfirmedOrder(t *testing.T) {
	s, now := fixture()
	s.Tasks[0].Plan, s.Tasks[1].Plan = s.Tasks[1].Plan, s.Tasks[0].Plan
	prepareDraft(&s, now)
	if s.NextDraft == nil || len(s.NextDraft.Changes) != 2 || s.NextDraft.Changes[0].TaskID != "b" || s.NextDraft.Changes[1].TaskID != "a" {
		t.Fatal("precomputed snooze proposal ignored manually confirmed order")
	}
}

func TestMemoryTransactionRollsBack(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.Update(t.Context(), "u", func(s *State) error { s.BalanceSeconds = 0; return errors.New("failed") })
	if err == nil {
		t.Fatal("missing failure")
	}
	s, _ := store.Read(t.Context(), "u")
	if s.BalanceSeconds != 3600 {
		t.Fatal("failed mutation was persisted")
	}
}
