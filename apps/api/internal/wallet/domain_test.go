package wallet

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)

func account(balance, deferred int, mode Mode) Account {
	a, _ := NewAccount(t0, time.UTC)
	a.Balance, a.Deferred, a.Mode = balance, deferred, mode
	return a
}

func TestGrantCapsAndDefers(t *testing.T) {
	cases := []struct {
		name            string
		balance, defer_ int
		kind            Kind
		night           bool
		wantSeconds     int
		wantDeferred    bool
	}{
		{"normal", 600, 0, KindRegistration, false, 300, false},
		{"near cap", 1790, 0, KindRegistration, false, 10, false},
		{"deferred counts against cap", 1500, 290, KindRegistration, false, 10, false},
		{"at cap", 1800, 0, KindRegistration, false, 0, false},
		{"night defers", 600, 0, KindRegistration, true, 300, true},
		{"sleep bonus never deferred", 600, 0, KindSleep, true, 300, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := account(c.balance, c.defer_, ModeRewards).Grant(c.kind, "k", 300, t0, c.night)
			if e.Seconds != c.wantSeconds || e.Deferred != c.wantDeferred {
				t.Fatalf("got %+v", e)
			}
		})
	}
}

func TestWorkReward(t *testing.T) {
	cases := []struct {
		name                       string
		mode                       Mode
		balance, todayWork, reward int
		work                       int
		want                       int
	}{
		{"5 minutes = 1 minute", ModeRewards, 600, 0, 0, 300, 60},
		{"remainder carried", ModeRewards, 600, 120, 0, 180, 60},
		{"under 5 minutes", ModeRewards, 600, 300, 60, 240, 0},
		{"daily cap", ModeRewards, 600, 0, 1180, 600, 20},
		{"balance cap", ModeRewards, 1790, 0, 0, 300, 10},
		{"fixed mode pays nothing", ModeFixed, 600, 0, 0, 300, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := account(c.balance, 0, c.mode)
			a.Today.Work, a.Today.Reward = c.todayWork, c.reward
			if e := a.WorkReward("s", "t", "x", c.work, t0, false); e.Seconds != c.want || e.WorkSeconds != c.work {
				t.Fatalf("got %+v", e)
			}
		})
	}
}

func TestReleaseCapsAndSettlesAll(t *testing.T) {
	a := account(1500, 600, ModeRewards)
	e, ok := a.Release("n", t0)
	if !ok || e.Seconds != 300 || e.SettledSeconds != 600 {
		t.Fatalf("got %+v", e)
	}
	a.Apply(e)
	if a.Balance != 1800 || a.Deferred != 0 {
		t.Fatalf("account %+v", a)
	}
	if _, ok := a.Release("n2", t0); ok {
		t.Fatal("nothing to release")
	}
}

func TestRefillByMode(t *testing.T) {
	cases := []struct {
		mode           Mode
		limit, balance int
		deferred       int
		want           int
	}{
		{ModeRewards, 1800, 240, 0, 360},
		{ModeRewards, 1800, 900, 0, 0},
		{ModeRewards, 1800, 240, 300, 60},
		{ModeFixed, 1200, 100, 0, 1100},
		{ModeReminder, 1800, 0, 0, 0},
		{ModeSelf, 1800, 0, 0, 0},
	}
	for _, c := range cases {
		a := account(c.balance, c.deferred, c.mode)
		a.DailyLimit = c.limit
		e, ok := a.Refill(t0, false)
		if e.Seconds != c.want || ok != (c.want > 0) {
			t.Fatalf("%s: got %+v", c.mode, e)
		}
	}
}

func TestAvailable(t *testing.T) {
	a := account(1440, 0, ModeRewards)
	a.Today.Viewed = 1710
	if got := a.Available(); got != 90 {
		t.Fatalf("available = %d", got)
	}
	a.Mode = ModeSelf
	if got := a.Available(); got != UnlimitedAvailableSeconds {
		t.Fatalf("self available = %d", got)
	}
}

func TestViewingDebitsOnlyLimitedModes(t *testing.T) {
	if e := account(8, 0, ModeRewards).Viewing("r", 10, t0); e.Seconds != -8 || e.ViewedSeconds != 10 {
		t.Fatalf("rewards %+v", e)
	}
	if e := account(8, 0, ModeSelf).Viewing("r", 10, t0); e.Seconds != 0 || e.ViewedSeconds != 10 {
		t.Fatalf("self %+v", e)
	}
}

func TestTodaysWorkClipsToDay(t *testing.T) {
	a, _ := NewAccount(time.Date(2026, 11, 9, 0, 10, 0, 0, time.UTC), time.UTC)
	got := a.TodaysWork([]Interval{{Start: time.Date(2026, 11, 8, 23, 50, 0, 0, time.UTC), End: time.Date(2026, 11, 9, 0, 15, 0, 0, time.UTC)}})
	if got != 900 {
		t.Fatalf("got %d", got)
	}
}

func TestEntryIDFitsContractPattern(t *testing.T) {
	if got := EntryID(KindDaily, "2026-11-08"); got != "daily-2026-11-08" {
		t.Fatalf("got %s", got)
	}
	long := EntryID(KindViewing, strings.Repeat("a", 80))
	if len(long) > 80 || !strings.HasPrefix(long, "viewing-") {
		t.Fatalf("got %s", long)
	}
	if EntryID(KindViewing, "a:b") == "viewing-a:b" {
		t.Fatal("unsafe key kept")
	}
}

func TestRolloverKeepsFourteenDays(t *testing.T) {
	a, _ := NewAccount(t0, time.UTC)
	for i := 1; i <= 20; i++ {
		if !a.Rollover(t0.AddDate(0, 0, i), time.UTC) {
			t.Fatal("expected rollover")
		}
	}
	if len(a.History) != 14 || a.Rollover(t0.AddDate(0, 0, 20), time.UTC) {
		t.Fatalf("history %d", len(a.History))
	}
}

func TestRebuildMatchesApply(t *testing.T) {
	a, initial := NewAccount(t0, time.UTC)
	entries := []Entry{initial}
	for _, e := range []Entry{
		a.Grant(KindRegistration, "t1", 300, t0, false),
		a.Grant(KindRegistration, "t2", 300, t0, true),
		{ID: "viewing-x", Kind: KindViewing, Seconds: -100, ViewedSeconds: 100, At: t0},
	} {
		entries = append(entries, a.Apply(e))
	}
	r, _ := a.Release("n", t0)
	entries = append(entries, a.Apply(r))
	p := Rebuild(entries)
	if p.Balance != a.Balance || p.Deferred != a.Deferred || p.LastSeq != a.LastSeq || p.Days[a.Day].Viewed != a.Today.Viewed {
		t.Fatalf("rebuild %+v account %+v", p, a)
	}
}
