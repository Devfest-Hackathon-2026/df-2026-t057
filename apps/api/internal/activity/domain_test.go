package activity

import (
	"testing"
	"time"
)

func TestWeekBoundariesAndDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 0, 1, 0, 0, loc)
	sunday := time.Date(2026, 11, 8, 23, 59, 0, 0, loc)
	scheduled := now.Add(-5 * time.Minute)
	events := []Event{{Kind: Start, At: now, ScheduledAt: &scheduled}, {Kind: Snooze, At: sunday}, {Kind: Work, At: sunday, Seconds: 60}, {Kind: Work, At: now.Add(time.Hour), Seconds: 99}}
	weeks := Summarize(events, now, loc)
	if weeks[11].Start != "2026-11-09" || weeks[11].Starts != 1 || weeks[11].OnTime != 1 || weeks[11].WorkSeconds != 0 || weeks[10].Snoozes != 1 || weeks[10].WorkSeconds != 60 {
		t.Fatalf("weeks=%+v", weeks)
	}
}
