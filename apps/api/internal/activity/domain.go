// Package activity stores immutable completion and action records independently of tasks.
package activity

import "time"

type Kind string

const (
	Start  Kind = "start"
	Snooze Kind = "snooze"
	Work   Kind = "work"
)

type Event struct {
	ID          string     `json:"id" firestore:"id"`
	TaskID      string     `json:"taskId" firestore:"taskId"`
	Kind        Kind       `json:"kind" firestore:"kind"`
	At          time.Time  `json:"at" firestore:"at"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty" firestore:"scheduledAt,omitempty"`
	Seconds     int        `json:"seconds" firestore:"seconds"`
}
type Completion struct {
	TaskID string    `json:"taskId" firestore:"taskId"`
	Title  string    `json:"title" firestore:"title"`
	At     time.Time `json:"at" firestore:"at"`
}
type Week struct {
	Start       string
	Starts      int
	OnTime      int
	Snoozes     int
	WorkSeconds int
}

func Summarize(events []Event, now time.Time, loc *time.Location) []Week {
	today := now.In(loc)
	monday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(int(today.Weekday())+6)%7)
	weeks := make([]Week, 12)
	index := map[string]int{}
	for i := range weeks {
		start := monday.AddDate(0, 0, (i-11)*7)
		weeks[i].Start = start.Format(time.DateOnly)
		index[weeks[i].Start] = i
	}
	for _, e := range events {
		d := e.At.In(loc)
		start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(int(d.Weekday())+6)%7)
		i, ok := index[start.Format(time.DateOnly)]
		if !ok || e.At.After(now) {
			continue
		}
		w := &weeks[i]
		switch e.Kind {
		case Start:
			w.Starts++
			if e.ScheduledAt != nil && !e.At.After(e.ScheduledAt.Add(5*time.Minute)) {
				w.OnTime++
			}
		case Snooze:
			w.Snoozes++
		case Work:
			w.WorkSeconds += e.Seconds
		}
	}
	return weeks
}
