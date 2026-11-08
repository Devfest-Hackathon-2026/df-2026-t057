package sleep

import (
	"testing"
	"time"
)

var jst = func() *time.Location { l, _ := time.LoadLocation("Asia/Tokyo"); return l }()

func TestNightWindow(t *testing.T) {
	cases := []struct {
		bed, wake  string
		day        time.Time
		start, end time.Time
	}{
		{"23:30", "07:00", time.Date(2026, 11, 8, 12, 0, 0, 0, jst), time.Date(2026, 11, 8, 22, 30, 0, 0, jst), time.Date(2026, 11, 9, 7, 0, 0, 0, jst)},
		{"01:00", "08:30", time.Date(2026, 11, 8, 12, 0, 0, 0, jst), time.Date(2026, 11, 9, 0, 0, 0, 0, jst), time.Date(2026, 11, 9, 8, 30, 0, 0, jst)},
	}
	for _, c := range cases {
		n := Schedule{Bedtime: c.bed, Wake: c.wake}.NightOf(c.day, jst)
		if !n.Start.Equal(c.start) || !n.End.Equal(c.end) || n.Key != "2026-11-08" {
			t.Fatalf("%s: %+v", c.bed, n)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		bed, wake string
		ok        bool
	}{{"23:30", "07:00", true}, {"22:00", "23:00", false}, {"21:00", "11:00", false}, {"25:00", "07:00", false}, {"7:00", "15:00", false}}
	for _, c := range cases {
		if err := (Schedule{Bedtime: c.bed, Wake: c.wake}).Validate(); (err == nil) != c.ok {
			t.Fatalf("%s-%s: %v", c.bed, c.wake, err)
		}
	}
}

func TestSettle(t *testing.T) {
	sched := Schedule{Bedtime: "23:30", Wake: "07:00"}
	since := time.Date(2026, 11, 7, 12, 0, 0, 0, jst)
	s := &Sleep{Schedule: sched, Since: since}
	if _, ok := Settle(s, time.Date(2026, 11, 9, 6, 0, 0, 0, jst), jst); ok {
		t.Fatal("no settlement during the night")
	}
	st, ok := Settle(s, time.Date(2026, 11, 9, 7, 30, 0, 0, jst), jst)
	if !ok || !st.Kept || st.Night.Key != "2026-11-08" {
		t.Fatalf("settle %+v %v", st, ok)
	}
	settled := st.Apply(*s)
	if _, ok := Settle(&settled, time.Date(2026, 11, 9, 8, 0, 0, 0, jst), jst); ok || settled.KeptNights != 1 {
		t.Fatal("second settlement")
	}
	watched := *s
	watched.WatchedNight = "2026-11-08"
	if st, _ := Settle(&watched, time.Date(2026, 11, 9, 7, 30, 0, 0, jst), jst); st.Kept {
		t.Fatal("watched night kept")
	}
}

func TestNewScheduleDoesNotJudgeLastNight(t *testing.T) {
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, jst)
	s := Set(nil, Schedule{Bedtime: "23:30", Wake: "07:00"}, now, jst)
	if _, ok := Settle(&s, now, jst); ok {
		t.Fatal("bedtime set this morning must not pay for last night")
	}
}
