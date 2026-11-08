package records

import (
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

func TestCompute(t *testing.T) {
	jst, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, jst)
	done := now.Add(-time.Hour)
	yesterday := now.AddDate(0, 0, -1)
	started, scheduled := now.Add(-2*time.Hour), now.Add(-2*time.Hour-4*time.Minute)
	late, lateScheduled := now.Add(-3*time.Hour), now.Add(-4*time.Hour)
	ended := now.Add(-time.Minute)
	in := Input{
		Now: now, Location: jst,
		Tasks: []task.Task{
			{ID: "a", Status: task.StatusCompleted, CompletedAt: &done, StartedAt: &started, ScheduledStartAt: &scheduled},
			{ID: "b", Status: task.StatusCompleted, CompletedAt: &yesterday, StartedAt: &late, ScheduledStartAt: &lateScheduled},
			{ID: "c", Status: task.StatusPending, Plan: task.Plan{Start: now.Add(-time.Hour)}},
		},
		Work:     []wallet.Entry{{Kind: wallet.KindWork, Seconds: 75, At: done}, {Kind: wallet.KindRegistration, Seconds: 300, At: done}},
		Sessions: []viewing.Session{{ID: "v1", StartedAt: now.Add(-time.Hour), EndedAt: &ended}, {ID: "v2", ExtendFrom: "v1", StartedAt: now.Add(-30 * time.Minute)}, {ID: "old", StartedAt: now.AddDate(0, 0, -8)}},
		Account:  wallet.Account{Today: wallet.DayTotals{Day: "2026-11-08", Viewed: 10, Work: 375}, History: []wallet.DayTotals{{Day: "2026-11-07", Viewed: 5}}},
	}
	r := Compute(in)
	last := r.Days[len(r.Days)-1]
	if len(r.Days) != 84 || last.Date != "2026-11-08" || last.Completed != 1 || last.EarnedSeconds != 75 || r.Days[82].Completed != 1 {
		t.Fatalf("days %+v %+v", last, r.Days[82])
	}
	p := r.Practice
	if r.TotalCompleted != 2 || r.TotalEarned != 75 || p.ViewingSessions != 2 || p.EndedSessions != 1 || p.Extensions != 1 ||
		p.StartedTasks != 2 || p.OnTimeStarts != 1 || p.MissedStarts != 1 || len(p.History) != 2 || p.History[0].WorkSeconds != 375 {
		t.Fatalf("records %+v", r)
	}
}
