package records

import (
	"testing"
	"time"
)

func TestWeekUsesProfileZoneAndCapturedPlan(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC) // Monday midnight in Japan.
	on := now.Add(2 * time.Minute)
	late := now.Add(-time.Minute)
	h := History{Completions: []Completion{{TaskID: "a", At: now}, {TaskID: "old", At: now.Add(-time.Nanosecond)}}, Events: []Event{{Kind: Started, At: on, ScheduledAt: &now}, {Kind: Started, At: now.Add(10 * time.Minute), ScheduledAt: &late}, {Kind: Snoozed, At: now}}}
	w, err := Summarize(h, now, "Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if !w.Start.Equal(now) || w.Completed != 1 || w.Starts != 2 || w.OnTime != 1 || w.Snoozes != 1 {
		t.Fatalf("week: %#v", w)
	}
}
