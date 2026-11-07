package app

import (
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
)

func TestCalendarAccountMustMatchSignedInGoogleAccount(t *testing.T) {
	for _, tc := range []struct {
		expected, actual string
		allowed          bool
	}{
		{"google-a", "google-a", true},
		{"google-a", "google-b", false},
		{"", "google-a", false},
		{"google-a", "", false},
	} {
		if err := validateCalendarAccount(tc.expected, tc.actual); (err == nil) != tc.allowed {
			t.Errorf("account %q -> %q: %v", tc.expected, tc.actual, err)
		}
	}
}

func TestCalendarNotesSurvivePlanReplacement(t *testing.T) {
	description := "本人のメモ\n\n[DOPAGAKI]\n古い予定\n[/DOPAGAKI]\n追加のメモ"
	if stripPlan(description) != "本人のメモ\n\n\n追加のメモ" {
		t.Fatalf("user notes changed: %q", stripPlan(description))
	}
	event := &calendar.Event{Summary: "課題", Description: "メモ", Start: &calendar.EventDateTime{DateTime: "2026-11-06T13:00:00+09:00"}, End: &calendar.EventDateTime{DateTime: "2026-11-06T14:00:00+09:00"}}
	hash := eventHash(event)
	event.Description += "\n\n[DOPAGAKI]\n予定\n[/DOPAGAKI]"
	if eventHash(event) != hash {
		t.Fatal("own plan update causes a sync loop")
	}
	event.Summary = "変更された課題"
	if eventHash(event) == hash {
		t.Fatal("user edits ignored")
	}
}

func TestCalendarRetryEditAndCancellation(t *testing.T) {
	now := time.Now()
	end := now.Add(2 * time.Hour)
	event := &calendar.Event{Id: "manual-event", Summary: "手入力の課題", Etag: "v1", Created: now.Add(-time.Hour).Format(time.RFC3339),
		Start: &calendar.EventDateTime{DateTime: end.Add(-30 * time.Minute).Format(time.RFC3339)}, End: &calendar.EventDateTime{DateTime: end.Format(time.RFC3339)}}
	s := NewState(now)
	applyCalendarEvents(&s, []*calendar.Event{event}, nil, now)
	if s.BalanceSeconds != initialSeconds+300 || len(s.Tasks) != 1 {
		t.Fatal("calendar registration changed viewing time or failed to create a task")
	}
	id := s.Tasks[0].ID
	revision := s.PlanRevision
	applyCalendarEvents(&s, []*calendar.Event{event, event}, nil, now)
	if s.BalanceSeconds != initialSeconds+300 || len(s.Tasks) != 1 || s.PlanRevision != revision {
		t.Fatal("duplicate webhook changed the plan or rewards")
	}
	event.Summary = "変更した課題"
	event.Etag = "v2"
	applyCalendarEvents(&s, []*calendar.Event{event}, nil, now)
	if s.Tasks[0].Title != event.Summary || s.BalanceSeconds != initialSeconds+300 || s.PlanRevision <= revision {
		t.Fatal("user edit was not applied exactly once")
	}
	s.Session = &Session{TaskID: id, StartedAt: now}
	event.Status = "cancelled"
	applyCalendarEvents(&s, []*calendar.Event{event}, nil, now)
	if s.Session != nil || len(s.Tasks) != 0 {
		t.Fatal("cancelled work left an orphaned timer")
	}
	event.Status = "confirmed"
	applyCalendarEvents(&s, []*calendar.Event{event}, nil, now)
	if s.Tasks[0].ID != id || s.BalanceSeconds != initialSeconds+300 {
		t.Fatal("restoring an event duplicated registration credit")
	}
}

func TestCalendarIgnoresAllDayAndRecurringEvents(t *testing.T) {
	s := NewState(time.Now())
	events := []*calendar.Event{
		{Id: "all-day", Start: &calendar.EventDateTime{Date: "2026-12-01"}, End: &calendar.EventDateTime{Date: "2026-12-02"}},
		{Id: "recurring", Recurrence: []string{"RRULE:FREQ=DAILY"}},
	}
	applyCalendarEvents(&s, events, nil, time.Now())
	if len(s.Tasks) != 0 || s.BalanceSeconds != initialSeconds {
		t.Fatal("unsupported event imported")
	}
}

func TestCalendarContentPatchAndPlanOnlyPatch(t *testing.T) {
	end := time.Now().Add(4 * time.Hour).Truncate(time.Second)
	task := Task{Title: "変更後", Notes: "新しいメモ", Deadline: end, Minutes: 45}
	task.Plan = planAt(task, end.Add(-45*time.Minute), end, "新しい手順")
	event := &calendar.Event{Summary: "変更前", Description: "Google側のメモ", Start: &calendar.EventDateTime{DateTime: end.Add(-30 * time.Minute).Format(time.RFC3339)}, End: &calendar.EventDateTime{DateTime: end.Format(time.RFC3339)}}
	planOnly := calendarPatch(task, event, time.UTC)
	if planOnly.Summary != "" || planOnly.Start != nil || planOnly.End != nil || stripPlan(planOnly.Description) != "Google側のメモ" {
		t.Fatal("plan edit changed original event fields")
	}
	task.CalendarInputPending = true
	content := calendarPatch(task, event, time.UTC)
	if content.Summary != task.Title || stripPlan(content.Description) != task.Notes || content.Start.DateTime != task.Plan.Start.Format(time.RFC3339) || content.End.DateTime != end.Format(time.RFC3339) {
		t.Fatal("content edit did not update the dedicated event")
	}
	updated := *event
	updated.Summary, updated.Description, updated.Start, updated.End = content.Summary, content.Description, content.Start, content.End
	retry := calendarPatch(task, &updated, time.UTC)
	if retry.Description != content.Description || eventHash(&updated) == eventHash(event) {
		t.Fatal("calendar retry duplicates plan text or hides content change")
	}
}

func TestCalendarUpdateRejectsExternalEditsAndAllowsCompletedRetry(t *testing.T) {
	end := time.Now().Add(4 * time.Hour).Truncate(time.Second)
	event := &calendar.Event{Summary: "元の課題", Description: "元のメモ", Etag: "v1", Start: &calendar.EventDateTime{DateTime: end.Add(-30 * time.Minute).Format(time.RFC3339)}, End: &calendar.EventDateTime{DateTime: end.Format(time.RFC3339)}}
	task := Task{Title: "変更後", Notes: "変更したメモ", Deadline: end, Minutes: 45, CalendarETag: event.Etag, InputHash: eventHash(event), CalendarInputPending: true}
	task.Plan = planAt(task, end.Add(-45*time.Minute), end, "手順")
	patch, desiredHash, err := calendarUpdate(task, event, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	event.Etag, event.Summary = "external", "Googleで変更した別の内容"
	if _, _, err = calendarUpdate(task, event, time.UTC); err != ErrConflict {
		t.Fatal("external content edit overwritten")
	}
	task.CalendarInputPending = false
	if _, _, err = calendarUpdate(task, event, time.UTC); err != ErrConflict {
		t.Fatal("plan-only export ignored an external edit")
	}
	task.CalendarInputPending = true
	event.Summary, event.Description, event.Start, event.End = patch.Summary, patch.Description, patch.Start, patch.End
	if _, hash, err := calendarUpdate(task, event, time.UTC); err != nil || hash != desiredHash {
		t.Fatal("completed remote write could not be retried after a lost response")
	}
}

func TestCalendarHashTreatsEquivalentTimeZonesAsSameEvent(t *testing.T) {
	event := &calendar.Event{Summary: "課題", Description: "メモ", Start: &calendar.EventDateTime{DateTime: "2026-12-01T12:00:00+09:00"}, End: &calendar.EventDateTime{DateTime: "2026-12-01T13:00:00+09:00"}}
	hash := eventHash(event)
	event.Start.DateTime, event.End.DateTime = "2026-12-01T03:00:00.000Z", "2026-12-01T04:00:00Z"
	if eventHash(event) != hash {
		t.Fatal("equivalent Google response times changed the input hash")
	}
}
