package app

import (
	"errors"
	"time"
)

const (
	initialSeconds        = 10 * 60
	balanceCapSeconds     = 30 * 60
	dailyRewardCapSeconds = 20 * 60
)

type TimePolicy struct {
	Version           int       `json:"version"`
	Mode              string    `json:"mode"`
	DailyLimitSeconds int       `json:"dailyLimitSeconds"`
	TimeZone          string    `json:"timeZone"`
	Day               string    `json:"day"`
	DayStartedAt      time.Time `json:"dayStartedAt"`
	ResetAt           time.Time `json:"resetAt"`
	ViewedSeconds     int       `json:"viewedSeconds"`
	RewardSeconds     int       `json:"rewardSeconds"`
	WorkSeconds       int       `json:"workSeconds"`
	History           []TimeDay `json:"history"`
}

type TimeDay struct {
	Day           string `json:"day"`
	ViewedSeconds int    `json:"viewedSeconds"`
	WorkSeconds   int    `json:"workSeconds"`
}

type WorkInterval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type ViewingSession struct {
	ExtendFrom      string     `json:"extendFrom,omitempty"`
	ID              string     `json:"id"`
	StartedAt       time.Time  `json:"startedAt"`
	PlannedSeconds  int        `json:"plannedSeconds"`
	ConsumedSeconds int        `json:"consumedSeconds"`
	LastReportAt    time.Time  `json:"lastReportAt"`
	EndedAt         *time.Time `json:"endedAt,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

func newTimePolicy(now time.Time, zone string) *TimePolicy {
	loc, _ := time.LoadLocation(zone)
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return &TimePolicy{Version: 1, Mode: "rewards", DailyLimitSeconds: 1800, TimeZone: zone,
		Day: local.Format("2006-01-02"), DayStartedAt: start, ResetAt: start.AddDate(0, 0, 1), History: []TimeDay{}}
}

func (s *State) advanceTimePolicy(now time.Time) bool {
	p := s.TimePolicy
	if p == nil || now.Before(p.ResetAt) {
		return false
	}
	p.History = append(p.History, TimeDay{Day: p.Day, ViewedSeconds: p.ViewedSeconds, WorkSeconds: p.WorkSeconds})
	if len(p.History) > 14 {
		p.History = p.History[len(p.History)-14:]
	}
	next := newTimePolicy(now, p.TimeZone)
	p.Day, p.DayStartedAt, p.ResetAt = next.Day, next.DayStartedAt, next.ResetAt
	p.ViewedSeconds, p.RewardSeconds, p.WorkSeconds = 0, 0, 0
	base := initialSeconds
	if p.Mode == "fixed" {
		base = p.DailyLimitSeconds
	}
	if p.Mode == "rewards" || p.Mode == "fixed" {
		seconds := max(0, min(base, balanceCapSeconds)-s.BalanceSeconds)
		if seconds > 0 {
			s.grant("daily", p.Day, seconds, now)
		}
	}
	return true
}

func (s *State) viewingAvailable() int {
	if s.TimePolicy == nil {
		return s.BalanceSeconds
	}
	if s.TimePolicy.Mode == "reminder" || s.TimePolicy.Mode == "self" {
		return 600
	}
	return max(0, min(s.BalanceSeconds, s.TimePolicy.DailyLimitSeconds-s.TimePolicy.ViewedSeconds))
}

func (s *State) closeWorkInterval(now time.Time) {
	x := s.Session
	if x == nil || x.RunningSince == nil {
		return
	}
	used := 0
	for _, interval := range x.Intervals {
		used += int(interval.End.Sub(interval.Start).Seconds())
	}
	end := minTime(now, x.RunningSince.Add(time.Duration(max(0, x.LimitSeconds-used))*time.Second))
	if end.After(*x.RunningSince) {
		x.Intervals = append(x.Intervals, WorkInterval{Start: *x.RunningSince, End: end})
	}
	x.RunningSince = nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s *State) confirmWork(now time.Time) {
	x, p := s.Session, s.TimePolicy
	if x == nil || p == nil {
		return
	}
	s.closeWorkInterval(now)
	for _, entry := range s.Ledger {
		if entry.ID == "work:"+x.ID {
			return
		}
	}
	seconds := 0
	for _, interval := range x.Intervals {
		start, end := maxTime(interval.Start, p.DayStartedAt), minTime(interval.End, p.ResetAt)
		seconds += max(0, int(end.Sub(start).Seconds()))
	}
	previous := p.WorkSeconds / 300
	p.WorkSeconds += seconds
	reward := 0
	if p.Mode == "rewards" {
		reward = max(0, min((p.WorkSeconds/300-previous)*60, dailyRewardCapSeconds-p.RewardSeconds, balanceCapSeconds-s.BalanceSeconds))
	}
	p.RewardSeconds += reward
	s.BalanceSeconds += reward
	s.Ledger = append(s.Ledger, LedgerEntry{ID: "work:" + x.ID, Kind: "work", Seconds: reward, WorkSeconds: seconds, TaskID: x.TaskID, At: now})
}

func (s *State) startViewing(id string, seconds int, extendFrom string, now time.Time) error {
	if s.TimePolicy == nil {
		return errors.New("新しい視聴時間の設定を有効にしてください")
	}
	if len(id) < 8 || len(id) > 80 || (seconds != 300 && seconds != 600) {
		return errors.New("視聴する長さを5分か10分から選んでください")
	}
	if s.ViewingSession != nil && s.ViewingSession.ID == id {
		return nil
	}
	for _, x := range s.ViewingHistory {
		if x.ID == id {
			return nil
		}
	}
	if s.ViewingSession != nil && s.ViewingSession.EndedAt == nil {
		return errors.New("視聴中の区切りを引き継いでください")
	}
	if extendFrom != "" && (s.ViewingSession == nil || s.ViewingSession.ID != extendFrom) {
		return ErrConflict
	}
	available := s.viewingAvailable()
	if available <= 0 {
		return errors.New("今日の視聴枠を使い切りました。休憩や次の作業へ移りましょう")
	}
	if s.ViewingSession != nil {
		s.ViewingHistory = append(s.ViewingHistory, *s.ViewingSession)
		if len(s.ViewingHistory) > 100 {
			s.ViewingHistory = s.ViewingHistory[len(s.ViewingHistory)-100:]
		}
	}
	s.ViewingSession = &ViewingSession{ID: id, ExtendFrom: extendFrom, StartedAt: now, LastReportAt: now, PlannedSeconds: min(seconds, available)}
	s.ViewingReceipts = map[string]bool{}
	return nil
}

func (s *State) endViewing(id, reason string, now time.Time) error {
	x := s.ViewingSession
	if x == nil || x.ID != id {
		for _, old := range s.ViewingHistory {
			if old.ID == id {
				return nil
			}
		}
		return ErrConflict
	}
	if x.EndedAt == nil {
		x.EndedAt, x.Reason = &now, reason
	}
	return nil
}
