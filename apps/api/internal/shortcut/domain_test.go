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
