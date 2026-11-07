package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/api/calendar/v3"
)

// Sleep is the bedtime the user chose. Rewards earned in the hour before bedtime are held until morning.
type Sleep struct {
	Bedtime string    `json:"bedtime"`
	Wake    string    `json:"wake"`
	Since   time.Time `json:"since"`
	EventID string    `json:"eventId,omitempty"`
	Watched string    `json:"watchedNight,omitempty"`
	Settled string    `json:"settledNight,omitempty"`
	Kept    int       `json:"keptNights"`
}

// sleepWindDown is how long before bedtime the night starts.
const sleepWindDown = time.Hour

// sleepBonus is the morning reward for a night without Shorts.
const sleepBonus = 900

func clock(value string) (int, int, bool) {
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

func validateSleep(bedtime, wake string) error {
	bh, bm, ok1 := clock(bedtime)
	wh, wm, ok2 := clock(wake)
	if !ok1 || !ok2 {
		return errors.New("就寝と起床の時刻をHH:MMで入力してください")
	}
	hours := float64((wh*60+wm-bh*60-bm+24*60)%(24*60)) / 60
	if hours < 4 || hours > 12 {
		return errors.New("睡眠時間は4〜12時間で設定してください")
	}
	return nil
}

// night returns the night that begins on the local date of day: from wind-down before bedtime to wake.
// Bedtimes before noon belong to the following calendar day (e.g. 01:00 after a late evening).
func (s Sleep) night(day time.Time, loc *time.Location) (key string, start, end time.Time) {
	bh, bm, _ := clock(s.Bedtime)
	wh, wm, _ := clock(s.Wake)
	y, m, d := day.In(loc).Date()
	bed := time.Date(y, m, d, bh, bm, 0, 0, loc)
	if bh < 12 {
		bed = bed.AddDate(0, 0, 1)
	}
	end = time.Date(bed.Year(), bed.Month(), bed.Day(), wh, wm, 0, 0, loc)
	if !end.After(bed) {
		end = end.AddDate(0, 0, 1)
	}
	return time.Date(y, m, d, 0, 0, 0, 0, loc).Format(time.DateOnly), bed.Add(-sleepWindDown), end
}

// nightAt reports the night that contains now, if any.
func (st *State) nightAt(now time.Time) (string, bool) {
	if st.Sleep == nil {
		return "", false
	}
	for _, back := range []int{0, -1} {
		key, start, end := st.Sleep.night(now.AddDate(0, 0, back), location(*st))
		if !now.Before(start) && now.Before(end) {
			return key, true
		}
	}
	return "", false
}

// lastNight is the most recent night that has already ended.
func (st *State) lastNight(now time.Time) (string, time.Time, time.Time) {
	for back := -1; ; back-- {
		key, start, end := st.Sleep.night(now.AddDate(0, 0, back), location(*st))
		if !now.Before(end) {
			return key, start, end
		}
	}
}

// Morning is what the user received when the night was settled.
type Morning struct {
	Night    string `json:"night"`
	Kept     bool   `json:"kept"`
	Bonus    int    `json:"bonusSeconds"`
	Released int    `json:"releasedSeconds"`
}

// settleMorning releases the rewards held overnight and grants the bonus for a night without Shorts, once per night.
func (st *State) settleMorning(now time.Time) *Morning {
	if st.Sleep == nil {
		return nil
	}
	if _, in := st.nightAt(now); in {
		return nil
	}
	key, start, _ := st.lastNight(now)
	if st.Sleep.Settled == key {
		return nil
	}
	morning := &Morning{Night: key}
	for i := range st.Ledger {
		if st.Ledger[i].Deferred {
			st.Ledger[i].Deferred = false
			seconds := max(0, min(st.Ledger[i].Seconds, balanceCapSeconds-st.BalanceSeconds))
			st.BalanceSeconds += seconds
			morning.Released += seconds
		}
	}
	if st.Sleep.Since.Before(start) && st.Sleep.Watched != key {
		morning.Kept = true
		morning.Bonus = st.grant("sleep", key, sleepBonus, now)
		st.Sleep.Kept++
	} else {
		st.Sleep.Kept = 0
	}
	st.Sleep.Settled = key
	return morning
}

func (s *Server) setSleep(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Bedtime string `json:"bedtime"`
		Wake    string `json:"wake"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx, id := r.Context(), uid(r)
	current, err := s.Store.Read(ctx, id)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if in.Bedtime == "" {
		if current.Sleep != nil && current.Sleep.EventID != "" && s.Calendar != nil {
			_ = s.Calendar.DeleteEvent(ctx, id, current.Sleep.EventID)
		}
		st, err := s.Store.Update(ctx, id, func(st *State) error {
			st.Sleep = nil
			return nil
		})
		respond(w, st, err)
		return
	}
	if err := validateSleep(in.Bedtime, in.Wake); err != nil {
		respond(w, nil, err)
		return
	}
	next := Sleep{Bedtime: in.Bedtime, Wake: in.Wake, Since: time.Now()}
	if current.Sleep != nil {
		next.Since, next.EventID, next.Watched, next.Settled, next.Kept = current.Sleep.Since, current.Sleep.EventID, current.Sleep.Watched, current.Sleep.Settled, current.Sleep.Kept
		if current.Sleep.Bedtime != in.Bedtime || current.Sleep.Wake != in.Wake {
			next.Since = time.Now()
		}
	}
	if s.Calendar != nil && current.CalendarConnected {
		if next.EventID, err = s.Calendar.UpsertSleep(ctx, id, next, location(current)); err != nil {
			respond(w, nil, err)
			return
		}
	}
	st, err := s.Store.Update(ctx, id, func(st *State) error {
		st.Sleep = &next
		return nil
	})
	respond(w, st, err)
}

func (s *Server) morning(w http.ResponseWriter, r *http.Request) {
	var morning *Morning
	st, err := s.Store.Update(r.Context(), uid(r), func(st *State) error {
		morning = st.settleMorning(time.Now())
		return nil
	})
	respond(w, map[string]any{"state": st, "morning": morning}, err)
}

// UpsertSleep keeps one daily recurring bedtime event in the DOPAGAKI calendar. Sync ignores recurring events.
func (c *Calendar) UpsertSleep(ctx context.Context, uid string, sleep Sleep, loc *time.Location) (string, error) {
	svc, err := c.service(ctx, uid)
	if err != nil {
		return "", err
	}
	conn, err := c.connection(ctx, uid)
	if err != nil {
		return "", err
	}
	_, start, end := sleep.night(time.Now(), loc)
	start = start.Add(sleepWindDown)
	hash := sha256.Sum256([]byte(uid + ":sleep"))
	event := &calendar.Event{
		Id: hex.EncodeToString(hash[:16]), Summary: "🌙 就寝（DOPAGAKI）",
		Description: fmt.Sprintf("%sに寝て%sに起きる予定です。就寝1時間前からは、課題の報酬を翌朝に受け取ります。", sleep.Bedtime, sleep.Wake),
		Start:       &calendar.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: loc.String()},
		End:         &calendar.EventDateTime{DateTime: end.Format(time.RFC3339), TimeZone: loc.String()},
		Recurrence:  []string{"RRULE:FREQ=DAILY"}, Transparency: "transparent",
		ExtendedProperties: &calendar.EventExtendedProperties{Private: map[string]string{"dopagakiSleep": "1"}},
	}
	if sleep.EventID != "" {
		if _, err := svc.Events.Update(conn.CalendarID, sleep.EventID, event).Context(ctx).Do(); err == nil {
			return sleep.EventID, nil
		}
	}
	created, err := svc.Events.Insert(conn.CalendarID, event).Context(ctx).Do()
	if err != nil {
		if updated, uerr := svc.Events.Update(conn.CalendarID, event.Id, event).Context(ctx).Do(); uerr == nil {
			return updated.Id, nil
		}
		return "", sourceError("就寝のカレンダー登録", err)
	}
	return created.Id, nil
}
