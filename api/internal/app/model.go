package app

import (
	"errors"
	"math"
	"sort"
	"time"
)

var ErrConflict = errors.New("予定が更新されています。最新の内容を確認してください")
var ErrNotFound = errors.New("課題が見つかりません")

type Step struct {
	Title string    `json:"title"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type Plan struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Steps  []Step    `json:"steps"`
	Reason string    `json:"reason"`
}
type Task struct {
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	ScheduledStartAt     *time.Time `json:"scheduledStartAt,omitempty"`
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Notes                string     `json:"notes"`
	Why                  string     `json:"why,omitempty"`
	Deadline             time.Time  `json:"deadline"`
	Minutes              int        `json:"minutes"`
	URL                  string     `json:"url"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"createdAt"`
	CompletedAt          *time.Time `json:"completedAt,omitempty"`
	Plan                 Plan       `json:"plan"`
	CalendarEventID      string     `json:"calendarEventId,omitempty"`
	CalendarInputPending bool       `json:"calendarInputPending,omitempty"`
	CalendarETag         string     `json:"calendarEtag,omitempty"`
	InputHash            string     `json:"inputHash,omitempty"`
}
type Profile struct {
	Name     string `json:"name"`
	Context  string `json:"context"`
	TimeZone string `json:"timeZone"`
}
type Settings struct {
	SnoozeMinutes int    `json:"snoozeMinutes"`
	Mode          string `json:"mode"`
	Sound         bool   `json:"sound"`
	ReducedMotion bool   `json:"reducedMotion"`
	Grime         bool   `json:"grime"`
}
type Session struct {
	PriorSeconds  int            `json:"priorSeconds,omitempty"`
	ID            string         `json:"id,omitempty"`
	LimitSeconds  int            `json:"limitSeconds,omitempty"`
	RunningSince  *time.Time     `json:"runningSince,omitempty"`
	Intervals     []WorkInterval `json:"intervals,omitempty"`
	TaskID        string         `json:"taskId"`
	StartedAt     time.Time      `json:"startedAt"`
	PausedAt      *time.Time     `json:"pausedAt,omitempty"`
	PausedSeconds int            `json:"pausedSeconds"`
}
type LedgerEntry struct {
	WorkSeconds int       `json:"workSeconds,omitempty"`
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Seconds     int       `json:"seconds"`
	TaskID      string    `json:"taskId,omitempty"`
	At          time.Time `json:"at"`
}
type State struct {
	TimePolicy            *TimePolicy      `json:"timePolicy,omitempty"`
	ViewingSession        *ViewingSession  `json:"viewingSession,omitempty"`
	ViewingHistory        []ViewingSession `json:"viewingHistory,omitempty"`
	Revision              int              `json:"revision"`
	PlanRevision          int              `json:"planRevision"`
	Profile               Profile          `json:"profile"`
	Settings              Settings         `json:"settings"`
	BalanceSeconds        int              `json:"balanceSeconds"`
	Tasks                 []Task           `json:"tasks"`
	Session               *Session         `json:"session,omitempty"`
	Ledger                []LedgerEntry    `json:"ledger"`
	CalendarConnected     bool             `json:"calendarConnected"`
	CalendarSync          string           `json:"calendarSync"`
	LastSync              *time.Time       `json:"lastSync,omitempty"`
	NextDraft             *Draft           `json:"nextDraft,omitempty"`
	LastViewingAt         *time.Time       `json:"lastViewingAt,omitempty"`
	ViewingReceipts       map[string]bool  `json:"viewingReceipts,omitempty"`
	MemoryReceipts        map[string]bool  `json:"memoryReceipts,omitempty"`
	AssistantContextSince *time.Time       `json:"assistantContextSince,omitempty"`
}
type PlanChange struct {
	TaskID          string  `json:"taskId"`
	Title           string  `json:"title"`
	Before          Plan    `json:"before"`
	After           Plan    `json:"after"`
	OriginalMinutes int     `json:"originalMinutes"`
	PlannedMinutes  float64 `json:"plannedMinutes"`
}
type Draft struct {
	ID           string       `json:"id"`
	TaskID       string       `json:"taskId"`
	BaseRevision int          `json:"baseRevision"`
	CreatedAt    time.Time    `json:"createdAt"`
	Start        time.Time    `json:"start"`
	Changes      []PlanChange `json:"changes"`
	Reason       string       `json:"reason"`
}

func NewState(now time.Time) State {
	return State{
		Revision: 1, PlanRevision: 1,
		Profile:        Profile{Name: "あなた", TimeZone: "Asia/Tokyo"},
		Settings:       Settings{SnoozeMinutes: 10, Mode: "pachinko"},
		TimePolicy:     newTimePolicy(now, "Asia/Tokyo"),
		BalanceSeconds: initialSeconds,
		Tasks:          []Task{},
		Ledger:         []LedgerEntry{{ID: "initial", Kind: "initial", Seconds: initialSeconds, At: now}},
		CalendarSync:   "disconnected", ViewingReceipts: map[string]bool{},
	}
}
func (s *State) task(id string) (*Task, error) {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			return &s.Tasks[i], nil
		}
	}
	return nil, ErrNotFound
}
func (s *State) grant(kind, taskID string, seconds int, now time.Time) {
	id := kind + ":" + taskID
	for _, entry := range s.Ledger {
		if entry.ID == id {
			return
		}
	}
	if kind == "registration" {
		seconds = max(0, min(seconds, balanceCapSeconds-s.BalanceSeconds))
	}
	s.BalanceSeconds += seconds
	s.Ledger = append(s.Ledger, LedgerEntry{ID: id, Kind: kind, Seconds: seconds, TaskID: taskID, At: now})
}
func (s *State) pending() []Task {
	tasks := []Task{}
	for _, t := range s.Tasks {
		if t.Status == "pending" {
			tasks = append(tasks, t)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Plan.Start.Before(tasks[j].Plan.Start) })
	return tasks
}
func (s *State) replan() {
	tasks := s.pending()
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].Deadline.Before(tasks[j].Deadline) })
	next := time.Time{}
	for i := len(tasks) - 1; i >= 0; i-- {
		end := tasks[i].Deadline
		if !next.IsZero() && next.Before(end) {
			end = next
		}
		start := end.Add(-time.Duration(tasks[i].Minutes) * time.Minute)
		t, _ := s.task(tasks[i].ID)
		t.Plan = planAt(*t, start, end, "締切と必要時間から逆算した予定です。")
		next = start
	}
	s.PlanRevision++
}
func planAt(t Task, start, end time.Time, reason string) Plan {
	steps := []Step{{Title: t.Title, Start: start, End: end}}
	if len(t.Plan.Steps) > 0 {
		steps = nil
		original := t.Plan.End.Sub(t.Plan.Start)
		cursor := start
		for i, step := range t.Plan.Steps {
			last := end
			if i < len(t.Plan.Steps)-1 && original > 0 {
				last = cursor.Add(time.Duration(float64(end.Sub(start)) * float64(step.End.Sub(step.Start)) / float64(original)))
			}
			steps = append(steps, Step{Title: step.Title, Start: cursor, End: last})
			cursor = last
		}
	}
	return Plan{Start: start, End: end, Steps: steps, Reason: reason}
}
func MakeDraft(s State, taskID string, start, now time.Time) (Draft, error) {
	tasks := s.pending()
	index := -1
	for i, t := range tasks {
		if t.ID == taskID {
			index = i
			break
		}
	}
	if index < 0 {
		return Draft{}, ErrNotFound
	}
	tasks = tasks[index:]
	if start.Before(now) {
		return Draft{}, errors.New("開始時刻は現在より後にしてください")
	}
	scale := 1.0
	total := 0.0
	for _, t := range tasks {
		total += float64(t.Minutes * 60)
		scale = math.Min(scale, t.Deadline.Sub(start).Seconds()/total)
	}
	if scale <= 0 {
		return Draft{}, errors.New("締切を過ぎています。締切か対象の課題を見直してください")
	}
	reason := "作業時間を保って開始時刻を後ろへ移します。"
	if scale < 1 {
		reason = "締切に合わせて作業時間を短縮する案です。変更前後を確認してください。"
	}
	changes := []PlanChange{}
	cursor := start
	for _, t := range tasks {
		duration := time.Duration(float64(t.Minutes)*scale*60) * time.Second
		if duration < time.Minute {
			return Draft{}, errors.New("作業時間を十分に確保できません。課題か締切を見直してください")
		}
		end := cursor.Add(duration)
		changes = append(changes, PlanChange{TaskID: t.ID, Title: t.Title, Before: t.Plan, After: planAt(t, cursor, end, reason), OriginalMinutes: t.Minutes, PlannedMinutes: duration.Minutes()})
		cursor = end
	}
	return Draft{ID: newID(), TaskID: taskID, BaseRevision: s.PlanRevision, CreatedAt: now, Start: start, Changes: changes, Reason: reason}, nil
}
func ValidateChanges(changes []PlanChange, s State, now time.Time) error {
	if len(changes) == 0 {
		return errors.New("変更する予定がありません")
	}
	seen := map[string]bool{}
	last := time.Time{}
	for _, c := range changes {
		t, err := s.task(c.TaskID)
		if err != nil {
			return err
		}
		if seen[t.ID] || t.Status == "completed" {
			return errors.New("変更対象の課題を確認してください")
		}
		seen[t.ID] = true
		p := c.After
		if p.End.Sub(p.Start) < time.Minute || p.Start.Before(now.Add(-time.Minute)) {
			return errors.New("開始・終了時刻を確認してください")
		}
		if p.End.After(t.Deadline) {
			return errors.New("締切を超えています。時間か締切を見直してください")
		}
		if !last.IsZero() && p.Start.Before(last) {
			return errors.New("課題の時間が重なっています")
		}
		last = p.End
		if len(p.Steps) == 0 {
			return errors.New("作業の内容を入力してください")
		}
		cursor := p.Start
		for _, step := range p.Steps {
			if step.Title == "" || !step.Start.Equal(cursor) || !step.End.After(step.Start) || step.End.After(p.End) {
				return errors.New("作業の時間割が正しくありません")
			}
			cursor = step.End
		}
		if !cursor.Equal(p.End) {
			return errors.New("作業の終了時刻が一致しません")
		}
	}
	for _, c := range changes {
		for _, t := range s.Tasks {
			if seen[t.ID] || t.Status == "completed" {
				continue
			}
			if c.After.Start.Before(t.Plan.End) && t.Plan.Start.Before(c.After.End) {
				return errors.New("別の課題の予定と重なっています")
			}
		}
	}
	return nil
}
