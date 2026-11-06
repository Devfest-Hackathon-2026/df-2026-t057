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
	if s.BalanceSeconds != 4200 || len(s.Tasks) != 1 {
		t.Fatal("registration was not credited")
	}
	id := s.Tasks[0].ID
	revision := s.PlanRevision
	applyCalendarEvents(&s, []*calendar.Event{event, event}, nil, now)
	if s.BalanceSeconds != 4200 || len(s.Tasks) != 1 || s.PlanRevision != revision {
		t.Fatal("duplicate webhook changed the plan or rewards")
	}
	event.Summary = "変更した課題"
	event.Etag = "v2"
	applyCalendarEvents(&s, []*calendar.Event{event}, nil, now)
	if s.Tasks[0].Title != event.Summary || s.BalanceSeconds != 4200 || s.PlanRevision <= revision {
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
	if s.Tasks[0].ID != id || s.BalanceSeconds != 4200 {
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
	if len(s.Tasks) != 0 || s.BalanceSeconds != 3600 {
		t.Fatal("unsupported event imported")
	}
}
