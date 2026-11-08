package records

import "time"

type Kind string

const (
	Started      Kind = "started"
	Paused       Kind = "paused"
	Resumed      Kind = "resumed"
	Snoozed      Kind = "snoozed"
	WorkRecorded Kind = "work_recorded"
)

type Completion struct {
	TaskID           string     `json:"taskId"`
	Title            string     `json:"title"`
	At               time.Time  `json:"at"`
	EstimateSeconds  int        `json:"estimateSeconds"`
	WorkSeconds      *int       `json:"workSeconds,omitempty"`
	RewardSeconds    int        `json:"rewardSeconds"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	ScheduledStartAt *time.Time `json:"scheduledStartAt,omitempty"`
}
type Event struct {
	Seconds     int        `json:"seconds,omitempty"`
	ID          string     `json:"id"`
	TaskID      string     `json:"taskId"`
	Kind        Kind       `json:"kind"`
	At          time.Time  `json:"at"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
}
type History struct {
	Completions []Completion `json:"completions"`
	Events      []Event      `json:"events"`
}

type Week struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Completed   int       `json:"completed"`
	Starts      int       `json:"starts"`
	OnTime      int       `json:"onTime"`
	Snoozes     int       `json:"snoozes"`
	WorkSeconds int       `json:"workSeconds"`
}

func Summarize(h History, now time.Time, zone string) (Week, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Week{}, err
	}
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	offset := (int(day.Weekday()) + 6) % 7
	start := day.AddDate(0, 0, -offset)
	end := start.AddDate(0, 0, 7)
	w := Week{Start: start, End: end}
	for _, c := range h.Completions {
		if !c.At.Before(start) && c.At.Before(end) {
			w.Completed++
		}
	}
	for _, e := range h.Events {
		if e.At.Before(start) || !e.At.Before(end) {
			continue
		}
		switch e.Kind {
		case Started:
			w.Starts++
			if e.ScheduledAt != nil && !e.At.After(e.ScheduledAt.Add(5*time.Minute)) {
				w.OnTime++
			}
		case Snoozed:
			w.Snoozes++
		case WorkRecorded:
			w.WorkSeconds += e.Seconds
		}
	}
	return w, nil
}

type ActivityDay struct {
	Date      string `json:"date"`
	Completed int    `json:"completed"`
}
type Activity struct {
	Days      []ActivityDay `json:"days"`
	Completed int           `json:"completed"`
}

func BuildActivity(h History, now time.Time, zone string) (Activity, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Activity{}, err
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	start := today.AddDate(0, 0, -83)
	result := Activity{Days: make([]ActivityDay, 84)}
	positions := map[string]int{}
	for i := range result.Days {
		key := start.AddDate(0, 0, i).Format("2006-01-02")
		result.Days[i] = ActivityDay{Date: key}
		positions[key] = i
	}
	for _, c := range h.Completions {
		if index, ok := positions[c.At.In(loc).Format("2006-01-02")]; ok {
			result.Days[index].Completed++
			result.Completed++
		}
	}
	return result, nil
}
