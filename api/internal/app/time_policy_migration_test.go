package app

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestEveryExistingUserIsResetOnceAndKeepsTasks(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(time.Duration(version).String(), func(t *testing.T) {
			now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
			c := policyClient(t, &now)
			old := NewState(now.Add(-time.Hour))
			if version == 0 {
				old.TimePolicy = nil
			} else {
				old.TimePolicy.Version = 1
				old.TimePolicy.Mode = "self"
				old.TimePolicy.ViewedSeconds = 1200
			}
			old.BalanceSeconds = 7200
			old.Tasks = []Task{{ID: "one", Title: "残しておく課題", Status: "pending", Minutes: 30, Deadline: now.Add(time.Hour)}}
			old.replan()
			old.CalendarConnected = true
			old.Profile.Context = "残しておく作業傾向"
			_, err := c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error { *s = old; return nil })
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					code, _ := c.call("GET", "/api/state", nil)
					if code != 200 {
						t.Errorf("migration: %d", code)
					}
				})
			}
			wg.Wait()
			st := c.state("GET", "/api/state", nil)
			if st.BalanceSeconds != 600 || st.TimePolicy.Version != timePolicyVersion || st.TimePolicy.Mode != "rewards" || st.TimePolicy.DailyLimitSeconds != 1800 || st.TimePolicy.ViewedSeconds != 0 {
				t.Fatal("existing user did not enter the common rules with ten minutes")
			}
			if len(st.Tasks) != 1 || st.Tasks[0].Title != old.Tasks[0].Title || !st.Tasks[0].Plan.Start.Equal(old.Tasks[0].Plan.Start) || !st.CalendarConnected || st.Profile.Context != old.Profile.Context {
				t.Fatal("migration changed tasks, plans or profile")
			}
			if len(st.Ledger) != 2 || st.Ledger[1].Kind != "reset" || st.Ledger[1].Seconds != -6600 {
				t.Fatal("reset was not recorded exactly once")
			}
			in := input()
			in.ID = "two"
			in.Deadline = now.Add(2 * time.Hour)
			st = c.state("POST", "/api/tasks", in)
			if st.BalanceSeconds != 900 {
				t.Fatal("registration did not give five minutes after the reset")
			}
			st = c.state("GET", "/api/state", nil)
			if st.BalanceSeconds != 900 || len(st.Ledger) != 3 {
				t.Fatal("reload reset the earned balance again")
			}
			other := c
			other.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
			_, err = c.store.Update(t.Context(), "demo-"+other.cookie.Value, func(s *State) error { *s = old; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if st := other.state("POST", "/api/tasks", in); st.BalanceSeconds != 900 {
				t.Fatal("writing before the first state read bypassed the reset")
			}
		})
	}
}

func TestResetRetainsLegacyTimerButDoesNotRewardPastWork(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	c := policyClient(t, &now)
	_, err := c.store.Update(t.Context(), "demo-"+c.cookie.Value, func(s *State) error {
		s.TimePolicy = nil
		s.BalanceSeconds = 7200
		s.Tasks = []Task{{ID: "one", Title: "進行中", Status: "active", Minutes: 30, Plan: Plan{Start: now.Add(-22 * time.Minute), End: now.Add(8 * time.Minute)}}}
		s.Session = &Session{TaskID: "one", StartedAt: now.Add(-22 * time.Minute)}
		s.ViewingSession = &ViewingSession{ID: "old-viewing", StartedAt: now.Add(-time.Minute), PlannedSeconds: 300}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st := c.state("GET", "/api/state", nil)
	if st.Session == nil || st.Session.ID == "" || st.Session.PriorSeconds != 1320 || st.Tasks[0].Status != "active" || st.ViewingSession != nil || len(st.ViewingHistory) != 1 || st.ViewingHistory[0].Reason != "reset" {
		t.Fatal("migration did not preserve the work timer or close the old viewing session")
	}
	now = now.Add(5 * time.Minute)
	st = c.state("POST", "/api/session/confirm", map[string]string{"id": st.Session.ID})
	if st.BalanceSeconds != 600 || st.TimePolicy.WorkSeconds != 180 {
		t.Fatal("past work was rewarded or the planned work limit was extended")
	}
}

func TestRegistrationRewardsRespectTheCapAndAreNotDeferred(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	s := NewState(now)
	s.BalanceSeconds = 1790
	s.grant("registration", "near-cap", 300, now)
	if s.BalanceSeconds != 1800 || s.Ledger[1].Seconds != 10 {
		t.Fatal("registration exceeded the balance cap")
	}
	s.grant("registration", "at-cap", 300, now)
	s.BalanceSeconds = 900
	s.grant("registration", "at-cap", 300, now)
	if s.BalanceSeconds != 900 || len(s.Ledger) != 3 {
		t.Fatal("a capped registration reward was deferred")
	}
}

func TestResetOnlyRewardsNewWorkInAnExistingSession(t *testing.T) {
	now := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	s := NewState(now.Add(-time.Hour))
	s.TimePolicy.Version = 1
	start := now.Add(-10 * time.Minute)
	s.Session = &Session{ID: "existing-work", TaskID: "one", StartedAt: start, RunningSince: &start, LimitSeconds: 1500}
	s.BalanceSeconds = 7200
	s.advanceTimePolicy(now)
	s.confirmWork(now.Add(5 * time.Minute))
	if s.TimePolicy.WorkSeconds != 300 || s.TimePolicy.RewardSeconds != 60 || s.BalanceSeconds != 660 {
		t.Fatal("work from before the reset was rewarded")
	}
}
