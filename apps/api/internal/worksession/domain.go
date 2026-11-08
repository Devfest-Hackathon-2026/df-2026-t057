// Package worksession は作業タイマー（開始・一時停止・再開・記録）を扱う。作業中の課題は常に1つ以下。
package worksession

import "time"

// MaxLimitSeconds は1回の作業で記録できる上限（25分）。
const MaxLimitSeconds = 25 * 60

// MinLimitSeconds は作業の上限の最短。
const MinLimitSeconds = 60

// Interval は作業した区間。
type Interval struct {
	Start time.Time `json:"start" firestore:"start"`
	End   time.Time `json:"end" firestore:"end"`
}

// Session は実行中の作業。
type Session struct {
	ID           string     `json:"id" firestore:"id"`
	TaskID       string     `json:"taskId" firestore:"taskId"`
	StartedAt    time.Time  `json:"startedAt" firestore:"startedAt"`
	LimitSeconds int        `json:"limitSeconds" firestore:"limitSeconds"`
	RunningSince *time.Time `json:"runningSince,omitempty" firestore:"runningSince,omitempty"`
	Intervals    []Interval `json:"intervals" firestore:"intervals"`
	PausedAt     *time.Time `json:"pausedAt,omitempty" firestore:"pausedAt,omitempty"`
}

// LimitFor は計画の長さから作業の上限を決める（1分〜25分）。
func LimitFor(plannedSeconds int) int {
	return min(MaxLimitSeconds, max(MinLimitSeconds, plannedSeconds))
}

// Start は新しい作業を始める。
func Start(id, taskID string, now time.Time, limit int) Session {
	return Session{ID: id, TaskID: taskID, StartedAt: now, LimitSeconds: limit, RunningSince: &now, Intervals: []Interval{}}
}

// Running はタイマーが動いているか。
func (s Session) Running() bool { return s.RunningSince != nil }

func (s Session) used() time.Duration {
	var d time.Duration
	for _, iv := range s.Intervals {
		d += iv.End.Sub(iv.Start)
	}
	return d
}

// runningEnd は、動いている区間を now で閉じたときの終わり（上限を超えない）。
func (s Session) runningEnd(now time.Time) time.Time {
	remaining := max(0, time.Duration(s.LimitSeconds)*time.Second-s.used())
	end := s.RunningSince.Add(remaining)
	if now.Before(end) {
		return now
	}
	return end
}

// Close は動いている区間を閉じる。放置したタイマーも上限までしか数えない。
func (s *Session) Close(now time.Time) {
	if s.RunningSince == nil {
		return
	}
	if end := s.runningEnd(now); end.After(*s.RunningSince) {
		s.Intervals = append(s.Intervals, Interval{Start: *s.RunningSince, End: end})
	}
	s.RunningSince = nil
}

// Pause は一時停止する。一時停止中なら何もしない（false）。
func (s *Session) Pause(now time.Time) bool {
	if s.PausedAt != nil {
		return false
	}
	s.Close(now)
	s.PausedAt = &now
	return true
}

// Resume は再開する。動いているなら何もしない（false）。
func (s *Session) Resume(now time.Time) bool {
	if s.PausedAt == nil && s.RunningSince != nil {
		return false
	}
	s.PausedAt = nil
	s.RunningSince = &now
	return true
}

// ElapsedSeconds は now の時点で記録された作業の秒数（上限まで）。
func (s Session) ElapsedSeconds(now time.Time) int {
	d := s.used()
	if s.RunningSince != nil {
		d += s.runningEnd(now).Sub(*s.RunningSince)
	}
	return min(s.LimitSeconds, int(d/time.Second))
}
