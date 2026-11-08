package shortcut

import (
	"testing"
	"time"
)

func TestRecordClose(t *testing.T) {
	open := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	used := AppOpens{Times: []time.Time{open}, Uses: []AppUse{{Start: open, End: open.Add(time.Minute)}}}
	cases := []struct {
		name    string
		opens   AppOpens
		at      time.Time
		ok      bool
		minutes int
	}{
		{"開いた記録が無い", AppOpens{}, open, false, 0},
		{"開いてから12分", AppOpens{Times: []time.Time{open}}, open.Add(12 * time.Minute), true, 12},
		{"1分未満", AppOpens{Times: []time.Time{open}}, open.Add(30 * time.Second), true, 0},
		{"ちょうど3時間", AppOpens{Times: []time.Time{open}}, open.Add(maxAppUse), true, 180},
		{"3時間を超えた", AppOpens{Times: []time.Time{open}}, open.Add(maxAppUse + time.Second), false, 0},
		{"開いた時刻より前", AppOpens{Times: []time.Time{open}}, open.Add(-time.Second), false, 0},
		{"同じ開いたにもう組がある", used, open.Add(5 * time.Minute), false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next, use, ok := c.opens.RecordClose(c.at)
			if ok != c.ok || use.Minutes() != c.minutes {
				t.Fatalf("ok=%v minutes=%d", ok, use.Minutes())
			}
			if !ok && len(next.Uses) != len(c.opens.Uses) {
				t.Fatal("changed without a use")
			}
		})
	}
}

func TestRecordKeepsUses(t *testing.T) {
	open := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	a := AppOpens{Times: []time.Time{open}, Uses: []AppUse{{Start: open, End: open.Add(time.Minute)}}}
	if got := a.Record(open.Add(time.Hour)); len(got.Uses) != 1 || len(got.Times) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestUseMinutes(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, jst)
	a := AppOpens{Uses: []AppUse{
		{Start: now.Add(-2 * time.Hour), End: now.Add(-2*time.Hour + 20*time.Minute)},
		{Start: now.Add(-26 * time.Hour), End: now.Add(-26*time.Hour + 10*time.Minute)},
		{Start: now.Add(-8 * 24 * time.Hour), End: now.Add(-8*24*time.Hour + 30*time.Minute)},
	}}
	if today, week := a.UseMinutes(now, jst); today != 20 || week != 30 {
		t.Fatalf("today=%d week=%d", today, week)
	}
}

func TestNextDueAndWaits(t *testing.T) {
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		available int
		limited   bool
		taskStart time.Time
		found     bool
		reason    DueReason
		first     time.Duration
	}{
		{"持ち時間8分・課題は2時間後", 8 * 60, true, now.Add(2 * time.Hour), true, DueBalance, 8 * time.Minute},
		{"持ち時間30分・課題は10分後", 30 * 60, true, now.Add(10 * time.Minute), true, DueTask, 10 * time.Minute},
		{"持ち時間0分でも最初は60秒待つ", 0, true, time.Time{}, true, DueBalance, FirstWaitMin},
		{"課題の開始を過ぎている", 30 * 60, true, now.Add(-time.Hour), true, DueTask, FirstWaitMin},
		{"持ち時間40分は15分で区切る", 40 * 60, true, time.Time{}, true, DueBalance, MaxWait},
		{"同じ時刻なら持ち時間", 10 * 60, true, now.Add(10 * time.Minute), true, DueBalance, 10 * time.Minute},
		{"残高を使わないモードで課題なし", 600, false, time.Time{}, false, "", 0},
		{"残高を使わないモードは課題だけ", 600, false, now.Add(20 * time.Minute), true, DueTask, MaxWait},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			due, found := NextDue(now, c.available, c.limited, c.taskStart)
			if found != c.found || due.Reason != c.reason {
				t.Fatalf("due %+v %v", due, found)
			}
			if got := FirstWait(due, found, now); got != c.first {
				t.Fatalf("first wait %v", got)
			}
		})
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		at     time.Duration
		found  bool
		action Action
		wait   time.Duration
	}{
		{"時刻になった", 0, true, ActionNotify, 0},
		{"少し手前もずれとして通知", dueTolerance, true, ActionNotify, 0},
		{"時刻が後ろにずれた", 5 * time.Minute, true, ActionWait, 5 * time.Minute},
		{"短い待ち直しは30秒", 10 * time.Second, true, ActionWait, minRewait},
		{"遠い時刻は15分で区切る", time.Hour, true, ActionWait, MaxWait},
		{"通知する時刻がない", 0, false, ActionNone, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			action, wait := Decide(Due{At: now.Add(c.at), Reason: DueBalance}, c.found, now)
			if action != c.action || wait != c.wait {
				t.Fatalf("%v %v", action, wait)
			}
		})
	}
}

func TestWatching(t *testing.T) {
	a := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	b := a.Add(time.Minute)
	cases := []struct {
		name   string
		opens  AppOpens
		opened time.Time
		want   bool
	}{
		{"開いたまま", AppOpens{Times: []time.Time{a}}, a, true},
		{"閉じた", AppOpens{Times: []time.Time{a}, Uses: []AppUse{{Start: a, End: b}}}, a, false},
		{"新しく開き直した", AppOpens{Times: []time.Time{a, b}}, a, false},
		{"通知済み", AppOpens{Times: []time.Time{a}}.MarkNotified(a), a, false},
		{"記録が無い", AppOpens{}, a, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.opens.Watching(c.opened); got != c.want {
				t.Fatalf("got %v", got)
			}
		})
	}
	if id, ok := ParseOpenID(FormatOpenID(a)); !ok || !id.Equal(a) {
		t.Fatalf("open id round trip %v %v", id, ok)
	}
	for _, bad := range []string{"", "x", "-1", "0"} {
		if _, ok := ParseOpenID(bad); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
}
