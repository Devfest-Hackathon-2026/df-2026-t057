// Package records は記録の画面の集計（日ごとの完了と報酬、自分で区切る練習）を、本人のタイムゾーンで計算する。
package records

import (
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/activity"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

const (
	// Days は日ごとの集計の日数（今日を含む）。
	Days = 84
	// PracticeWindow は練習の集計の期間。
	PracticeWindow = 7 * 24 * time.Hour
	// OnTimeWindow は「予定どおりに始めた」とみなす遅れの上限。
	OnTimeWindow = 5 * time.Minute
	historyLimit = 14
)

// Day は1日の集計。
type Day struct {
	Date          string
	Completed     int
	EarnedSeconds int
}

// History は1日の視聴と作業。
type History struct {
	Date          string
	ViewedSeconds int
	WorkSeconds   int
}

// Practice は過去7日の練習の集計。
type Practice struct {
	ViewingSessions int
	EndedSessions   int
	Extensions      int
	StartedTasks    int
	OnTimeStarts    int
	MissedStarts    int
	History         []History
}

// Records は記録の集計。
type Records struct {
	Days           []Day
	TotalCompleted int
	TotalEarned    int
	Practice       Practice
	Weeks          []activity.Week
}

// Input は集計の元。
type Input struct {
	Completions []activity.Completion
	Events      []activity.Event
	Now         time.Time
	Location    *time.Location
	Tasks       []task.Task
	// Work は作業の報酬の台帳の項目（すべての期間）。
	Work     []wallet.Entry
	Sessions []viewing.Session
	Account  wallet.Account
}

// Compute は記録を計算する。報酬は作業の記録で得た秒数（保留中を含む）。
func Compute(in Input) Records {
	loc := in.Location
	today := in.Now.In(loc)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(Days - 1))
	r := Records{Days: make([]Day, Days)}
	index := map[string]int{}
	for i := range Days {
		date := start.AddDate(0, 0, i).Format(time.DateOnly)
		r.Days[i] = Day{Date: date}
		index[date] = i
	}
	seen := map[string]bool{}
	for _, c := range in.Completions {
		seen[c.TaskID] = true
		r.TotalCompleted++
		if i, ok := index[c.At.In(loc).Format(time.DateOnly)]; ok {
			r.Days[i].Completed++
		}
	}
	for _, t := range in.Tasks {
		if seen[t.ID] {
			continue
		}
		if t.CompletedAt == nil {
			continue
		}
		r.TotalCompleted++
		if i, ok := index[t.CompletedAt.In(loc).Format(time.DateOnly)]; ok {
			r.Days[i].Completed++
		}
	}
	for _, e := range in.Work {
		if e.Kind != wallet.KindWork {
			continue
		}
		r.TotalEarned += e.Seconds
		if i, ok := index[e.At.In(loc).Format(time.DateOnly)]; ok {
			r.Days[i].EarnedSeconds += e.Seconds
		}
	}
	since := in.Now.Add(-PracticeWindow)
	p := Practice{}
	for _, x := range in.Sessions {
		if x.StartedAt.Before(since) {
			continue
		}
		p.ViewingSessions++
		if x.EndedAt != nil {
			p.EndedSessions++
		}
		if x.ExtendFrom != "" {
			p.Extensions++
		}
	}
	for _, t := range in.Tasks {
		if t.StartedAt != nil && !t.StartedAt.Before(since) {
			p.StartedTasks++
			if t.ScheduledStartAt != nil && !t.StartedAt.After(t.ScheduledStartAt.Add(OnTimeWindow)) {
				p.OnTimeStarts++
			}
		}
		if t.StartedAt == nil && t.Status == task.StatusPending && t.Plan.Start.Before(in.Now) && !t.Plan.Start.Before(since) {
			p.MissedStarts++
		}
	}
	p.History = append(p.History, History{Date: in.Account.Today.Day, ViewedSeconds: in.Account.Today.Viewed, WorkSeconds: in.Account.Today.Work})
	for i := len(in.Account.History) - 1; i >= 0 && len(p.History) < historyLimit; i-- {
		h := in.Account.History[i]
		p.History = append(p.History, History{Date: h.Day, ViewedSeconds: h.Viewed, WorkSeconds: h.Work})
	}
	r.Practice = p
	r.Weeks = activity.Summarize(in.Events, in.Now, in.Location)
	return r
}
