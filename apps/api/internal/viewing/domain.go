// Package viewing は視聴の区切り（何分見るかを決めてから見る）と、視聴した秒数の報告を扱う。
package viewing

import (
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// MaxReportSeconds は1回の報告の上限。
const MaxReportSeconds = 65

// EndReason は区切りを終えた理由。
type EndReason string

const (
	EndStop  EndReason = "stop"
	EndLimit EndReason = "limit"
)

// Valid は定義済みの値か。
func (r EndReason) Valid() bool { return r == EndStop || r == EndLimit }

// Session は視聴の区切り。
type Session struct {
	ID              string     `json:"id" firestore:"id"`
	ExtendFrom      string     `json:"extendFrom,omitempty" firestore:"extendFrom,omitempty"`
	StartedAt       time.Time  `json:"startedAt" firestore:"startedAt"`
	PlannedSeconds  int        `json:"plannedSeconds" firestore:"plannedSeconds"`
	ConsumedSeconds int        `json:"consumedSeconds" firestore:"consumedSeconds"`
	LastReportAt    time.Time  `json:"lastReportAt" firestore:"lastReportAt"`
	EndedAt         *time.Time `json:"endedAt,omitempty" firestore:"endedAt,omitempty"`
	EndReason       EndReason  `json:"endReason,omitempty" firestore:"endReason,omitempty"`
}

// Active は区切りの途中か。
func (s Session) Active() bool { return s.EndedAt == nil }

// State は区切りの状態（今の区切りと、最後に報告した時刻）。
type State struct {
	CurrentID     string     `json:"currentId,omitempty" firestore:"currentId,omitempty"`
	LastViewingAt *time.Time `json:"lastViewingAt,omitempty" firestore:"lastViewingAt,omitempty"`
}

// ValidChoice は区切りの長さの選択肢か。
func ValidChoice(seconds int, choices []int) bool {
	for _, c := range choices {
		if c == seconds {
			return true
		}
	}
	return false
}

// CheckStart は区切りを始められるかを確かめる。current は今の区切り（無ければ nil）。
func CheckStart(current *Session, extendFrom string, available int) error {
	if current != nil && current.Active() {
		return apperr.Conflict("viewing.session_active")
	}
	if extendFrom != "" && (current == nil || current.ID != extendFrom) {
		return apperr.Conflict("viewing.extend_mismatch")
	}
	if available <= 0 {
		return apperr.Conflict("viewing.exhausted")
	}
	return nil
}

// Charge は区切りの中での報告を、減らす秒数に直す。報告した秒数・前回の報告からの経過・区切りの残り・今日の残りの最小値。
// 区切りを使い切るか、今日の残りが無くなれば区切りを終える。
func Charge(s *Session, reported, available int, now time.Time) int {
	elapsed := max(0, int(now.Sub(s.LastReportAt)/time.Second))
	seconds := max(0, min(reported, elapsed, s.PlannedSeconds-s.ConsumedSeconds, available))
	s.ConsumedSeconds += seconds
	s.LastReportAt = now
	if s.ConsumedSeconds >= s.PlannedSeconds || available <= seconds {
		s.EndedAt, s.EndReason = &now, EndLimit
	}
	return seconds
}

// ChargeFree は区切りを使わないルール（self）での報告。前回の報告からの経過を超えない。
func ChargeFree(reported int, last *time.Time, now time.Time) int {
	seconds := reported
	if last != nil {
		seconds = min(seconds, max(0, int(now.Sub(*last)/time.Second)))
	}
	return max(0, seconds)
}
