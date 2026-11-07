package app

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func policyClient(t *testing.T, now *time.Time) testClient {
	store := NewMemoryStore()
	store.now = func() time.Time { return *now }
	s := &Server{Mode: "demo", Store: store, Planner: &Planner{}, Now: store.now}
	return testClient{store: store, handler: s.Handler(), cookie: &http.Cookie{Name: "dopagaki_demo", Value: "abcdef0123456789abcdef0123456789"}, t: t}
}

func TestDailyRefillOnceWithoutLoginOrMissedDayAccumulation(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 59, 0, 0, time.UTC) // 23:59 JST
	c := policyClient(t, &now)
	st := c.state("GET", "/api/state", nil)
	if st.BalanceSeconds != 600 || len(st.Ledger) != 1 {
		t.Fatal("initial grant must include the first daily refill")
	}
	_, _ = c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error { s.BalanceSeconds = 240; return nil })
	now = now.Add(2 * time.Minute)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			code, _ := c.call("GET", "/api/state", nil)
			if code != 200 {
				t.Errorf("read %d", code)
			}
		})
	}
	wg.Wait()
	st = c.state("GET", "/api/state", nil)
	if st.BalanceSeconds != 600 || len(st.Ledger) != 2 || st.Ledger[1].Seconds != 360 {
		t.Fatalf("daily refill duplicated: %+v", st)
	}
	_, _ = c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error { s.BalanceSeconds = 900; return nil })
	now = now.AddDate(0, 0, 4)
	st = c.state("GET", "/api/state", nil)
	if st.BalanceSeconds != 900 || len(st.Ledger) != 2 {
		t.Fatal("missed days accumulated or earned balance was removed")
	}
	profile := st.Profile
	profile.TimeZone = "Pacific/Honolulu"
	c.state("PUT", "/api/profile", profile)
	st = c.state("GET", "/api/state", nil)
	if st.TimePolicy.TimeZone != "Asia/Tokyo" || st.BalanceSeconds != 900 {
		t.Fatal("profile timezone changed the refill clock")
	}
}

func TestWorkConfirmationCountsPausedIntervalsAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	c := policyClient(t, &now)
	in := input()
	in.Deadline = now.Add(2 * time.Hour)
	c.state("POST", "/api/tasks", in)
	st := c.state("POST", "/api/tasks/one/start", nil)
	id := st.Session.ID
	now = now.Add(3 * time.Minute)
	c.state("POST", "/api/session/pause", nil)
	now = now.Add(40 * time.Minute)
	c.state("POST", "/api/session/pause", nil)
	now = now.Add(2 * time.Minute)
	for range 3 {
		st = c.state("POST", "/api/session/confirm", map[string]string{"id": id})
	}
	if st.BalanceSeconds != 660 || st.TimePolicy.WorkSeconds != 300 || st.Session != nil || st.Tasks[0].Status != "pending" {
		t.Fatalf("invalid work confirmation: %+v", st)
	}
	if len(st.Ledger) != 2 {
		t.Fatal("work receipt recorded more than once")
	}
	// A tiny next block uses the remainder; merely completing another task grants nothing.
	st = c.state("POST", "/api/tasks/one/start", nil)
	now = now.Add(4 * time.Minute)
	st = c.state("POST", "/api/tasks/one/complete", nil)
	if st.BalanceSeconds != 660 || st.TimePolicy.WorkSeconds != 540 {
		t.Fatal("work remainder was rounded up")
	}
}

func TestWorkSplittingCapsAndNoDeferredRewards(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	s := NewState(now)
	confirm := func(id string, seconds int) {
		start := now
		s.Session = &Session{ID: id, TaskID: "a", StartedAt: start, RunningSince: &start, LimitSeconds: seconds}
		now = now.Add(time.Duration(seconds) * time.Second)
		s.confirmWork(now)
	}
	confirm("small-1", 120)
	confirm("small-2", 180)
	if s.TimePolicy.WorkSeconds != 300 || s.BalanceSeconds != 660 {
		t.Fatal("small blocks were not combined")
	}
	s.BalanceSeconds = 1790
	confirm("cap", 300)
	if s.BalanceSeconds != 1800 || s.TimePolicy.RewardSeconds != 70 {
		t.Fatal("balance cap must grant the actual increment")
	}
	s.BalanceSeconds = 1000
	confirm("after-cap", 60)
	if s.BalanceSeconds != 1000 {
		t.Fatal("previously capped work was granted later")
	}
	for i := range 8 {
		confirm(time.Duration(i).String(), 1500)
	}
	if s.TimePolicy.RewardSeconds > 1200 || s.BalanceSeconds > 1800 {
		t.Fatal("reward cap exceeded")
	}
	// A long abandoned timer is capped at its planned interval.
	start := now
	s.Session = &Session{ID: "abandoned", RunningSince: &start, LimitSeconds: 300}
	before := s.TimePolicy.WorkSeconds
	s.confirmWork(now.Add(10 * time.Hour))
	if s.TimePolicy.WorkSeconds-before != 300 {
		t.Fatal("abandoned timer earned unplanned work")
	}
	s = NewState(now)
	for i := range 5 {
		s.BalanceSeconds = 0
		confirm("daily-cap-"+time.Duration(i).String(), 1500)
	}
	if s.TimePolicy.RewardSeconds != 1200 || s.BalanceSeconds != 0 {
		t.Fatal("daily reward cap must remain effective after spending the earned balance")
	}
}

func TestViewingReportCrossingMidnightIsBookedOnceOnReceiptDay(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 59, 55, 0, time.UTC)
	c := policyClient(t, &now)
	c.state("POST", "/api/viewing/start", map[string]any{"id": "midnight-viewing", "seconds": 300})
	now = now.Add(10 * time.Second)
	for range 3 {
		st := c.state("POST", "/api/viewing", map[string]any{"id": "midnight-receipt", "sessionId": "midnight-viewing", "seconds": 10})
		if st.TimePolicy.Day != "2026-11-08" || st.TimePolicy.ViewedSeconds != 10 || st.BalanceSeconds != 590 {
			t.Fatal("cross-day receipt was allocated twice or granted extra time")
		}
	}
}

func TestWorkCrossingMidnightOnlyCreditsCurrentDay(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 50, 0, 0, time.UTC)
	s := NewState(now)
	s.Session = &Session{ID: "midnight-work", RunningSince: &now, LimitSeconds: 1500}
	end := now.Add(25 * time.Minute)
	s.advanceTimePolicy(end)
	s.confirmWork(end)
	if s.TimePolicy.WorkSeconds != 900 || s.TimePolicy.RewardSeconds != 180 || s.BalanceSeconds != 780 {
		t.Fatalf("cross-day reward: %+v", s.TimePolicy)
	}
}

func TestWorkConfirmationIsUserScopedAndSafeForConcurrentRetries(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	c := policyClient(t, &now)
	in := input()
	in.Deadline = now.Add(time.Hour)
	c.state("POST", "/api/tasks", in)
	st := c.state("POST", "/api/tasks/one/start", nil)
	id := st.Session.ID
	other := c
	other.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	if code, _ := other.call("POST", "/api/session/confirm", map[string]string{"id": id}); code != 409 {
		t.Fatal("another account confirmed work")
	}
	now = now.Add(5 * time.Minute)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			code, _ := c.call("POST", "/api/session/confirm", map[string]string{"id": id})
			if code != 200 {
				t.Errorf("confirmation: %d", code)
			}
		})
	}
	wg.Wait()
	st = c.state("GET", "/api/state", nil)
	if st.BalanceSeconds != 660 || st.TimePolicy.WorkSeconds != 300 || len(st.Ledger) != 2 {
		t.Fatal("parallel confirmations granted duplicate time")
	}
	if st := other.state("GET", "/api/state", nil); st.BalanceSeconds != 600 || st.TimePolicy.WorkSeconds != 0 {
		t.Fatal("work reward leaked to another account")
	}
}

func TestViewingRequiresChoiceSharesSessionAndCapsBothBalances(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	c := policyClient(t, &now)
	if code, _ := c.call("POST", "/api/viewing", map[string]any{"id": "no-choice", "seconds": 10}); code != 400 {
		t.Fatal("viewing without a choice was accepted")
	}
	st := c.state("POST", "/api/viewing/start", map[string]any{"id": "viewing-one", "seconds": 300})
	if st.ViewingSession.PlannedSeconds != 300 {
		t.Fatal("choice not stored")
	}
	if code, _ := c.call("POST", "/api/viewing/start", map[string]any{"id": "other-tab", "seconds": 600}); code != 400 {
		t.Fatal("parallel session created")
	}
	// Simulate less remaining in both limits than the report, and concurrent retransmission.
	_, _ = c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error { s.BalanceSeconds = 8; s.TimePolicy.ViewedSeconds = 1795; return nil })
	now = now.Add(10 * time.Second)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			code, _ := c.call("POST", "/api/viewing", map[string]any{"id": "viewing-receipt", "sessionId": "viewing-one", "seconds": 10})
			if code != 200 {
				t.Errorf("debit %d", code)
			}
		})
	}
	wg.Wait()
	st = c.state("GET", "/api/state", nil)
	if st.BalanceSeconds != 3 || st.TimePolicy.ViewedSeconds != 1800 || st.ViewingSession.ConsumedSeconds != 5 || st.ViewingSession.EndedAt == nil {
		t.Fatal("viewing exceeded the daily limit or retry charged twice")
	}
	if code, _ := c.call("POST", "/api/viewing/start", map[string]any{"id": "after-limit", "seconds": 300}); code != 400 {
		t.Fatal("daily limit bypassed")
	}
	now = now.AddDate(0, 0, 1)
	st = c.state("POST", "/api/viewing/start", map[string]any{"id": "after-limit", "seconds": 300})
	if st.BalanceSeconds != 600 || st.TimePolicy.ViewedSeconds != 0 || st.ViewingSession.ID != "after-limit" {
		t.Fatal("next-day refill or session restart failed")
	}
}

func TestPolicyMigrationAndModesPreserveCreditAndDoNotRefillOnToggle(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	c := policyClient(t, &now)
	_, _ = c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error { s.TimePolicy = nil; s.BalanceSeconds = 7200; return nil })
	if st := c.state("GET", "/api/state", nil); st.TimePolicy != nil || st.BalanceSeconds != 7200 {
		t.Fatal("legacy user silently migrated")
	}
	for _, mode := range []string{"rewards", "fixed", "reminder", "self", "rewards"} {
		st := c.state("PUT", "/api/time-policy", map[string]any{"mode": mode, "dailyLimitSeconds": 1200})
		if st.BalanceSeconds != 7200 || st.TimePolicy.Mode != mode {
			t.Fatal("mode change removed credit or granted a refill")
		}
	}
	c.state("PUT", "/api/time-policy", map[string]any{"mode": "fixed", "dailyLimitSeconds": 1200})
	_, _ = c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error { s.BalanceSeconds = 20; return nil })
	now = now.AddDate(0, 0, 1)
	st := c.state("GET", "/api/state", nil)
	if st.BalanceSeconds != 1200 {
		t.Fatal("fixed budget was not replenished")
	}
	c.state("PUT", "/api/time-policy", map[string]any{"mode": "self", "dailyLimitSeconds": 1200})
	now = now.Add(time.Minute)
	st = c.state("POST", "/api/viewing", map[string]any{"id": "self-viewing", "seconds": 10})
	if st.BalanceSeconds != 1200 || st.TimePolicy.ViewedSeconds != 10 {
		t.Fatal("self management consumed earned balance or did not record use")
	}
}
