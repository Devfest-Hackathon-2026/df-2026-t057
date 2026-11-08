// Package aiquota は AI（Gemini）の利用上限を扱う（§2.14）。
//
// AI を呼ぶ前に、利用者ごとと全体の、1分あたり・1日（UTC）あたりの回数を1回分確保する。
// 上限に達していれば何も増やさずに断る（断った呼び出しは回数に数えない。全体の上限で断っても本人の回数は減らない）。
package aiquota

import "time"

// Limits は利用上限。どれかが1未満なら AI を使えない。
type Limits struct {
	UserPerMinute   int
	UserPerDay      int
	GlobalPerMinute int
	GlobalPerDay    int
}

// Valid はすべての上限が1以上か。
func (l Limits) Valid() bool {
	return l.UserPerMinute >= 1 && l.UserPerDay >= 1 && l.GlobalPerMinute >= 1 && l.GlobalPerDay >= 1
}

// Usage は1つの数え先（利用者か全体）の、今の区切りでの回数。
type Usage struct {
	Minute      time.Time `json:"minute" firestore:"minute"`
	MinuteCalls int       `json:"minuteCalls" firestore:"minuteCalls"`
	Day         time.Time `json:"day" firestore:"day"`
	DayCalls    int       `json:"dayCalls" firestore:"dayCalls"`
}

// roll は区切りが変わっていれば回数を0に戻す。分が変わっても日の回数は残す。
func (u Usage) roll(minute, day time.Time) Usage {
	if !u.Minute.Equal(minute) {
		u.Minute, u.MinuteCalls = minute, 0
	}
	if !u.Day.Equal(day) {
		u.Day, u.DayCalls = day, 0
	}
	return u
}

// Reserve は1回分を確保した後の回数を返す。上限に達していれば ok は false で、retryAt は超えた区切りのうち最も遅い終わり。
// 断ったときは user・global を変えずに返す（何も増やさない）。
func Reserve(user, global Usage, limits Limits, now time.Time) (nextUser, nextGlobal Usage, retryAt time.Time, ok bool) {
	minute, day := now.UTC().Truncate(time.Minute), now.UTC().Truncate(24*time.Hour)
	u, g := user.roll(minute, day), global.roll(minute, day)
	check := func(usage Usage, perMinute, perDay int) {
		if usage.MinuteCalls >= perMinute && minute.Add(time.Minute).After(retryAt) {
			retryAt = minute.Add(time.Minute)
		}
		if usage.DayCalls >= perDay && day.Add(24*time.Hour).After(retryAt) {
			retryAt = day.Add(24 * time.Hour)
		}
	}
	check(u, limits.UserPerMinute, limits.UserPerDay)
	check(g, limits.GlobalPerMinute, limits.GlobalPerDay)
	if !retryAt.IsZero() {
		return user, global, retryAt, false
	}
	u.MinuteCalls++
	u.DayCalls++
	g.MinuteCalls++
	g.DayCalls++
	return u, g, time.Time{}, true
}

// RetryAfter は retryAt までの待ち時間（秒に切り上げ、最短1秒）。
func RetryAfter(retryAt, now time.Time) time.Duration {
	wait := retryAt.Sub(now)
	if rounded := wait.Truncate(time.Second); rounded < wait {
		wait = rounded + time.Second
	}
	return max(wait, time.Second)
}
