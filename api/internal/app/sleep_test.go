package app

import (
	"encoding/json"
	"testing"
	"time"
)

func sleepState(t *testing.T, bedtime, wake string, since time.Time) State {
	t.Helper()
	state := NewState(since)
	state.Sleep = &Sleep{Bedtime: bedtime, Wake: wake, Since: since}
	return state
}

func TestSleepNightStartsAnHourBeforeBedtime(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	for _, tc := range []struct {
		bedtime, wake string
		start, end    time.Time
	}{
		{"23:30", "07:00", time.Date(2026, 11, 7, 22, 30, 0, 0, loc), time.Date(2026, 11, 8, 7, 0, 0, 0, loc)},
		{"01:00", "08:30", time.Date(2026, 11, 8, 0, 0, 0, 0, loc), time.Date(2026, 11, 8, 8, 30, 0, 0, loc)},
	} {
		key, start, end := Sleep{Bedtime: tc.bedtime, Wake: tc.wake}.night(time.Date(2026, 11, 7, 12, 0, 0, 0, loc), loc)
		if key != "2026-11-07" || !start.Equal(tc.start) || !end.Equal(tc.end) {
			t.Errorf("%s-%s: %s %v %v", tc.bedtime, tc.wake, key, start, end)
		}
	}
	for _, tc := range []struct {
		bedtime, wake string
		ok            bool
	}{{"23:30", "07:00", true}, {"22:00", "23:00", false}, {"21:00", "11:00", false}, {"25:00", "07:00", false}} {
		if (validateSleep(tc.bedtime, tc.wake) == nil) != tc.ok {
			t.Errorf("validate %s-%s", tc.bedtime, tc.wake)
		}
	}
}

func TestNightRewardsWaitForMorningAndKeptNightEarnsBonus(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	set := time.Date(2026, 11, 7, 12, 0, 0, 0, loc)
	state := sleepState(t, "23:30", "07:00", set)
	state.grant("completion", "evening", 1200, time.Date(2026, 11, 7, 20, 0, 0, 0, loc))
	state.grant("completion", "late", 1200, time.Date(2026, 11, 7, 23, 0, 0, 0, loc))
	if state.BalanceSeconds != 3600+1200 {
		t.Fatalf("a night reward was added immediately: %d", state.BalanceSeconds)
	}
	if m := state.settleMorning(time.Date(2026, 11, 8, 6, 0, 0, 0, loc)); m != nil {
		t.Fatal("settled before wake time")
	}
	morning := state.settleMorning(time.Date(2026, 11, 8, 7, 30, 0, 0, loc))
	if morning == nil || !morning.Kept || morning.Released != 1200 || morning.Bonus != sleepBonus || state.BalanceSeconds != 3600+2400+sleepBonus || state.Sleep.Kept != 1 {
		t.Fatalf("morning: %+v balance %d", morning, state.BalanceSeconds)
	}
	if state.settleMorning(time.Date(2026, 11, 8, 9, 0, 0, 0, loc)) != nil {
		t.Fatal("the same night was settled twice")
	}
	state.Sleep.Watched = "2026-11-08"
	if morning := state.settleMorning(time.Date(2026, 11, 9, 8, 0, 0, 0, loc)); morning == nil || morning.Kept || state.Sleep.Kept != 0 {
		t.Fatalf("a night with Shorts earned the bonus: %+v", morning)
	}
}

func TestBedtimeSetTonightDoesNotPayForLastNight(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	state := sleepState(t, "23:30", "07:00", time.Date(2026, 11, 8, 7, 10, 0, 0, loc))
	if morning := state.settleMorning(time.Date(2026, 11, 8, 7, 20, 0, 0, loc)); morning == nil || morning.Kept {
		t.Fatalf("bonus for a night before the bedtime existed: %+v", morning)
	}
}

func TestSleepEndpointsInDemo(t *testing.T) {
	c := client(t)
	if code, _ := c.call("PUT", "/api/sleep", map[string]string{"bedtime": "21:00", "wake": "10:00"}); code != 400 {
		t.Fatal("13 hours of sleep accepted")
	}
	state := c.state("PUT", "/api/sleep", map[string]string{"bedtime": "23:30", "wake": "07:00"})
	if state.Sleep == nil || state.Sleep.Bedtime != "23:30" {
		t.Fatalf("bedtime not saved: %+v", state.Sleep)
	}
	code, raw := c.call("POST", "/api/sleep/morning", nil)
	var out struct {
		State   State    `json:"state"`
		Morning *Morning `json:"morning"`
	}
	if code != 200 || json.Unmarshal(raw, &out) != nil || out.Morning != nil {
		t.Fatalf("morning check-in: %d %s", code, raw)
	}
	if out.State.Revision != state.Revision {
		t.Fatal("a check-in with nothing to settle wrote the state")
	}
	if state := c.state("PUT", "/api/sleep", map[string]string{"bedtime": "", "wake": ""}); state.Sleep != nil {
		t.Fatal("bedtime not cleared")
	}
}

func TestCompanionProposesBedtimeAsCard(t *testing.T) {
	_, c := assistantClient(t, func(raw string) any {
		return map[string]any{"reply": "23:30に寝る形でカードにしたよ。", "cards": []any{map[string]any{"kind": "sleep", "bedtime": "23:30", "wake": "07:00"}}}
	})
	turn := consultation(t, c, "sleep", "最近夜ふかししちゃう。23時半には寝たいな、7時起き")
	if turn.Status != "ready" || len(turn.Cards) != 1 || turn.Cards[0].Kind != "sleep" || turn.Cards[0].Sleep == nil || turn.Cards[0].Sleep.Bedtime != "23:30" {
		t.Fatalf("sleep card: %+v", turn)
	}
	if state := c.state("GET", "/api/state", nil); state.Sleep != nil {
		t.Fatal("bedtime saved without confirmation")
	}
}
