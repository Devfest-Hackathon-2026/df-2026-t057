package aiquota

import (
	"testing"
	"time"
)

func TestReserve(t *testing.T) {
	base := time.Date(2026, 11, 7, 23, 59, 0, 0, time.UTC)
	noon := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	limits := Limits{UserPerMinute: 1, UserPerDay: 2, GlobalPerMinute: 10, GlobalPerDay: 10}
	type step struct {
		at        time.Duration
		ok        bool
		retryAt   time.Time
		userDay   int
		globalDay int
	}
	tests := []struct {
		name   string
		base   time.Time
		limits Limits
		user   Usage
		global Usage
		steps  []step
	}{
		{
			name:   "分の上限と、日付が変わると日の回数が戻る",
			limits: limits,
			steps: []step{
				{at: 0, ok: true, userDay: 1, globalDay: 1},
				{at: 59 * time.Second, ok: false, retryAt: base.Add(time.Minute), userDay: 1, globalDay: 1},
				{at: time.Minute, ok: true, userDay: 1, globalDay: 1},
				{at: 2 * time.Minute, ok: true, userDay: 2, globalDay: 2},
				{at: 3 * time.Minute, ok: false, retryAt: base.Add(24*time.Hour + time.Minute), userDay: 2, globalDay: 2},
			},
		},
		{
			name:   "分が変わっても日の回数は残る",
			base:   noon,
			limits: Limits{UserPerMinute: 1, UserPerDay: 2, GlobalPerMinute: 10, GlobalPerDay: 10},
			user:   Usage{Minute: noon.Add(-time.Minute), MinuteCalls: 1, Day: noon.Truncate(24 * time.Hour), DayCalls: 1},
			steps: []step{
				{at: 0, ok: true, userDay: 2, globalDay: 1},
				{at: time.Minute, ok: false, retryAt: noon.Truncate(24 * time.Hour).Add(24 * time.Hour), userDay: 2, globalDay: 1},
			},
		},
		{
			name:   "全体の上限で断っても本人の回数は増えない",
			base:   noon,
			limits: Limits{UserPerMinute: 5, UserPerDay: 30, GlobalPerMinute: 1, GlobalPerDay: 300},
			global: Usage{Minute: noon, MinuteCalls: 1, Day: noon.Truncate(24 * time.Hour), DayCalls: 1},
			steps: []step{
				{at: 0, ok: false, retryAt: noon.Add(time.Minute), userDay: 0, globalDay: 1},
				{at: time.Minute, ok: true, userDay: 1, globalDay: 2},
			},
		},
		{
			name:   "分と日の両方を超えたら遅いほうの終わり",
			limits: Limits{UserPerMinute: 1, UserPerDay: 1, GlobalPerMinute: 10, GlobalPerDay: 10},
			user:   Usage{Minute: base, MinuteCalls: 1, Day: base.Truncate(24 * time.Hour), DayCalls: 1},
			steps:  []step{{at: 0, ok: false, retryAt: base.Truncate(24 * time.Hour).Add(24 * time.Hour), userDay: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, global := tt.user, tt.global
			start := tt.base
			if start.IsZero() {
				start = base
			}
			for i, s := range tt.steps {
				var retryAt time.Time
				var ok bool
				user, global, retryAt, ok = Reserve(user, global, tt.limits, start.Add(s.at))
				if ok != s.ok || !retryAt.Equal(s.retryAt) || user.DayCalls != s.userDay || global.DayCalls != s.globalDay {
					t.Fatalf("step %d: ok=%v retryAt=%v user=%+v global=%+v", i, ok, retryAt, user, global)
				}
			}
		})
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 30, 500_000_000, time.UTC)
	tests := []struct {
		retryAt time.Time
		want    time.Duration
	}{
		{now.Add(29500 * time.Millisecond), 30 * time.Second},
		{now.Add(30 * time.Second), 30 * time.Second},
		{now.Add(100 * time.Millisecond), time.Second},
		{now.Add(-time.Second), time.Second},
	}
	for _, tt := range tests {
		if got := RetryAfter(tt.retryAt, now); got != tt.want {
			t.Errorf("RetryAfter(%v) = %v, want %v", tt.retryAt.Sub(now), got, tt.want)
		}
	}
}

func TestLimitsValid(t *testing.T) {
	if !(Limits{1, 1, 1, 1}).Valid() || (Limits{1, 1, 0, 1}).Valid() {
		t.Fatal("Valid")
	}
}
