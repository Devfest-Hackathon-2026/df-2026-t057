// Package task は課題と計画（締切からの逆算、スヌーズの案、変更の検証）を扱う。
package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
)

// 入力と計画の上限。
const (
	MaxTasks          = 200
	MaxTitleRunes     = 200
	MaxNotesRunes     = 4000
	MaxWhyRunes       = 200
	MaxURLLength      = 2000
	MinMinutes        = 1
	MaxMinutes        = 720
	MaxSteps          = 20
	MaxStepTitleRunes = 100
	MaxNoteRunes      = 500
	// MinPlanSeconds は1つの課題に割り当てる最短の時間。
	MinPlanSeconds = 60
)

// Status は課題の状態。
type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)

// Reason は計画がこの形になった理由。表示の文言はクライアントが持つ。
type Reason string

const (
	ReasonBackward        Reason = "backward"
	ReasonAIDetailed      Reason = "ai_detailed"
	ReasonAILimitFallback Reason = "ai_limit_fallback"
	ReasonSnoozeKept      Reason = "snooze_kept"
	ReasonSnoozeShortened Reason = "snooze_shortened"
	ReasonEditReplanned   Reason = "edit_replanned"
	ReasonAIAdjusted      Reason = "ai_adjusted"
	ReasonManual          Reason = "manual"
)

// Valid は定義済みの値か。
func (r Reason) Valid() bool {
	switch r {
	case ReasonBackward, ReasonAIDetailed, ReasonAILimitFallback, ReasonSnoozeKept, ReasonSnoozeShortened,
		ReasonEditReplanned, ReasonAIAdjusted, ReasonManual:
		return true
	default:
		return false
	}
}

// Step は計画の手順。
type Step struct {
	Title string    `json:"title" firestore:"title"`
	Start time.Time `json:"start" firestore:"start"`
	End   time.Time `json:"end" firestore:"end"`
}

// Plan は課題の計画。
type Plan struct {
	Start  time.Time `json:"start" firestore:"start"`
	End    time.Time `json:"end" firestore:"end"`
	Steps  []Step    `json:"steps" firestore:"steps"`
	Reason Reason    `json:"reason" firestore:"reason"`
	// Note は AI が付けた補足。
	Note string `json:"note,omitempty" firestore:"note,omitempty"`
}

// Seconds は計画の長さ。
func (p Plan) Seconds() int { return int(p.End.Sub(p.Start) / time.Second) }

// Task は課題。
type Task struct {
	ID               string     `json:"id" firestore:"id"`
	Title            string     `json:"title" firestore:"title"`
	Notes            string     `json:"notes" firestore:"notes"`
	Why              string     `json:"why" firestore:"why"`
	Deadline         time.Time  `json:"deadline" firestore:"deadline"`
	Minutes          int        `json:"minutes" firestore:"minutes"`
	URL              string     `json:"url" firestore:"url"`
	Status           Status     `json:"status" firestore:"status"`
	CreatedAt        time.Time  `json:"createdAt" firestore:"createdAt"`
	StartedAt        *time.Time `json:"startedAt,omitempty" firestore:"startedAt,omitempty"`
	ScheduledStartAt *time.Time `json:"scheduledStartAt,omitempty" firestore:"scheduledStartAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty" firestore:"completedAt,omitempty"`
	Plan             Plan       `json:"plan" firestore:"plan"`
}

// PlannedSeconds は計画の長さ（最短1分）。作業タイマーの上限に使う。
func (t Task) PlannedSeconds() int { return max(MinPlanSeconds, t.Plan.Seconds()) }

// Input は本人が入力する課題の内容。
type Input struct {
	Title    string
	Notes    string
	Why      string
	Deadline time.Time
	Minutes  int
	URL      string
}

// ValidateInput は入力を確かめる。題名とやる理由は前後の空白を除いてから数える。
func ValidateInput(in Input, now time.Time) error {
	var fields []apperr.FieldError
	title := strings.TrimSpace(in.Title)
	if title == "" || utf8.RuneCountInString(title) > MaxTitleRunes {
		fields = append(fields, apperr.Field("title", "task.title_length"))
	}
	if in.Minutes < MinMinutes || in.Minutes > MaxMinutes {
		fields = append(fields, apperr.Field("minutes", "task.minutes_range"))
	}
	if in.Deadline.IsZero() {
		fields = append(fields, apperr.Field("deadline", "required"))
	} else if in.Deadline.Before(now) {
		fields = append(fields, apperr.Field("deadline", "task.deadline_past"))
	}
	if utf8.RuneCountInString(in.Notes) > MaxNotesRunes {
		fields = append(fields, apperr.Field("notes", "task.notes_length"))
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Why)) > MaxWhyRunes {
		fields = append(fields, apperr.Field("why", "task.why_length"))
	}
	if !validURL(in.URL) {
		fields = append(fields, apperr.Field("url", "task.url_scheme"))
	}
	if len(fields) > 0 {
		return apperr.Invalid("request.invalid", fields...)
	}
	return nil
}

func validURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > MaxURLLength {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

// NewTask は入力から課題を作る。計画は呼び出し側が決める。
func NewTask(id string, in Input, now time.Time) Task {
	t := Task{ID: id, Status: StatusPending, CreatedAt: now}
	t.ApplyInput(in)
	return t
}

// ApplyInput は入力を反映する。
func (t *Task) ApplyInput(in Input) {
	t.Title, t.Notes, t.Why = strings.TrimSpace(in.Title), in.Notes, strings.TrimSpace(in.Why)
	t.Deadline, t.Minutes, t.URL = in.Deadline.UTC(), in.Minutes, in.URL
}

// InputOf は課題の今の内容。
func InputOf(t Task) Input {
	return Input{Title: t.Title, Notes: t.Notes, Why: t.Why, Deadline: t.Deadline, Minutes: t.Minutes, URL: t.URL}
}

// PlanAt は [start, end] に収めた計画を作る。手順があれば長さの比で伸縮し、最後の手順は end で終える。
// 時刻は秒に切り捨てる（保存先の精度に左右されないように）。
func PlanAt(t Task, start, end time.Time, reason Reason) Plan {
	start, end = start.UTC().Truncate(time.Second), end.UTC().Truncate(time.Second)
	steps := []Step{{Title: t.Title, Start: start, End: end}}
	if len(t.Plan.Steps) > 0 {
		steps = make([]Step, 0, len(t.Plan.Steps))
		original := t.Plan.End.Sub(t.Plan.Start)
		cursor := start
		for i, step := range t.Plan.Steps {
			last := end
			if i < len(t.Plan.Steps)-1 && original > 0 {
				last = cursor.Add(time.Duration(float64(end.Sub(start)) * float64(step.End.Sub(step.Start)) / float64(original))).Truncate(time.Second)
				if last.After(end) {
					last = end
				}
			}
			steps = append(steps, Step{Title: step.Title, Start: cursor, End: last})
			cursor = last
		}
	}
	return Plan{Start: start, End: end, Steps: steps, Reason: reason, Note: t.Plan.Note}
}

// StepSpec は AI が提案した手順（題名と分数）。
type StepSpec struct {
	Title   string
	Minutes int
}

// DetailedPlan は AI の手順を、締切から逆算した時間に並べる。手順の合計は課題の分数と一致しなければならない。
func DetailedPlan(t Task, specs []StepSpec, note string) (Plan, bool) {
	if len(specs) == 0 || len(specs) > 8 {
		return Plan{}, false
	}
	total := 0
	for _, s := range specs {
		if strings.TrimSpace(s.Title) == "" || utf8.RuneCountInString(s.Title) > MaxStepTitleRunes || s.Minutes < 1 {
			return Plan{}, false
		}
		total += s.Minutes
	}
	if total != t.Minutes {
		return Plan{}, false
	}
	end := t.Deadline.UTC().Truncate(time.Second)
	cursor := end.Add(-time.Duration(t.Minutes) * time.Minute)
	plan := Plan{Start: cursor, End: end, Reason: ReasonAIDetailed}
	if utf8.RuneCountInString(note) <= MaxNoteRunes {
		plan.Note = strings.TrimSpace(note)
	}
	for _, s := range specs {
		next := cursor.Add(time.Duration(s.Minutes) * time.Minute)
		plan.Steps = append(plan.Steps, Step{Title: strings.TrimSpace(s.Title), Start: cursor, End: next})
		cursor = next
	}
	return plan, true
}

// BackwardPlan は手順を1つにした、締切から逆算した計画。
func BackwardPlan(t Task, reason Reason) Plan {
	t.Plan = Plan{}
	return PlanAt(t, t.Deadline.Add(-time.Duration(t.Minutes)*time.Minute), t.Deadline, reason)
}

// replanReason は組み直したときの理由。AI が具体化した手順は伸縮しても意味が変わらないので理由を残す。
func replanReason(r Reason) Reason {
	if r == ReasonAIDetailed || r == ReasonAILimitFallback {
		return r
	}
	return ReasonBackward
}

// Replan は未開始の課題を、締切の遅い順に後ろから詰めて並べ直す（作業中・完了は動かさない）。
// 計画が変わった課題の ID を返す。過去に始まる計画になることもある（逆算を優先する）。
func Replan(tasks []Task) []string {
	order := make([]int, 0, len(tasks))
	for i, t := range tasks {
		if t.Status == StatusPending {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return tasks[a].Deadline.Compare(tasks[b].Deadline) })
	var changed []string
	var next time.Time
	for k := len(order) - 1; k >= 0; k-- {
		t := &tasks[order[k]]
		end := t.Deadline
		if !next.IsZero() && next.Before(end) {
			end = next
		}
		start := end.Add(-time.Duration(t.Minutes) * time.Minute)
		plan := PlanAt(*t, start, end, replanReason(t.Plan.Reason))
		if !samePlan(plan, t.Plan) {
			changed = append(changed, t.ID)
		}
		t.Plan = plan
		next = start
	}
	return changed
}

func samePlan(a, b Plan) bool {
	return a.Start.Equal(b.Start) && a.End.Equal(b.End) && a.Reason == b.Reason && a.Note == b.Note &&
		slices.EqualFunc(a.Steps, b.Steps, func(x, y Step) bool { return x.Title == y.Title && x.Start.Equal(y.Start) && x.End.Equal(y.End) })
}

// Pending は未開始の課題を、計画の開始が早い順に返す（本人が確定した順番を保つため、締切ではなく開始で並べる）。
func Pending(tasks []Task) []Task {
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Status == StatusPending {
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b Task) int { return a.Plan.Start.Compare(b.Plan.Start) })
	return out
}

// Find は ID の課題を返す。
func Find(tasks []Task, id string) (*Task, bool) {
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i], true
		}
	}
	return nil, false
}

// DraftKind は変更案の種類。
type DraftKind string

const (
	DraftSnooze DraftKind = "snooze"
	DraftAdjust DraftKind = "adjust"
	DraftEdit   DraftKind = "edit"
)

// Change は1つの課題の変更前後。
type Change struct {
	TaskID          string
	Title           string
	Before          Plan
	After           Plan
	OriginalMinutes int
	PlannedSeconds  int
}

// Draft は計画の変更案。保存はしない。
type Draft struct {
	ID           string
	Kind         DraftKind
	TaskID       string
	BaseRevision int64
	CreatedAt    time.Time
	Start        *time.Time
	Shortened    bool
	Changes      []Change
}

// ChangeInput は確定する変更（クライアントから受け取る）。
type ChangeInput struct {
	TaskID string
	After  Plan
}

// draftID は案の ID。同じ版・同じ開始の案は同じ ID になる（読むたびに変わらないように）。
func draftID(kind DraftKind, taskID string, revision int64, start time.Time, extra string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d|%d|%s", kind, taskID, revision, start.Unix(), extra))
	return "d" + hex.EncodeToString(sum[:12])
}

// MakeDraft は taskID 以降の未開始の課題を start から順に並べる案を作る。締切に収まらなければ、比率を保って短縮する。
func MakeDraft(tasks []Task, taskID string, kind DraftKind, start, now time.Time, revision int64) (Draft, error) {
	pending := Pending(tasks)
	index := slices.IndexFunc(pending, func(t Task) bool { return t.ID == taskID })
	if index < 0 {
		if _, ok := Find(tasks, taskID); ok {
			return Draft{}, apperr.Conflict("task.not_editable")
		}
		return Draft{}, apperr.NotFound("task.not_found")
	}
	pending = pending[index:]
	start = start.UTC().Truncate(time.Second)
	if start.Before(now.Add(-time.Second)) {
		return Draft{}, apperr.Invalid("plan.start_in_past", apperr.Field("start", "in_past"))
	}
	scale := 1.0
	total := 0.0
	for _, t := range pending {
		total += float64(t.Minutes * 60)
		scale = math.Min(scale, t.Deadline.Sub(start).Seconds()/total)
	}
	if scale <= 0 {
		return Draft{}, apperr.Conflict("plan.deadline_passed")
	}
	reason := ReasonSnoozeKept
	if scale < 1 {
		reason = ReasonSnoozeShortened
	}
	changes := make([]Change, 0, len(pending))
	cursor := start
	for _, t := range pending {
		seconds := int(float64(t.Minutes) * scale * 60)
		if seconds < MinPlanSeconds {
			return Draft{}, apperr.Conflict("plan.insufficient_time")
		}
		end := cursor.Add(time.Duration(seconds) * time.Second)
		changes = append(changes, Change{TaskID: t.ID, Title: t.Title, Before: t.Plan, After: PlanAt(t, cursor, end, reason), OriginalMinutes: t.Minutes, PlannedSeconds: seconds})
		cursor = end
	}
	s := start
	return Draft{ID: draftID(kind, taskID, revision, start, ""), Kind: kind, TaskID: taskID, BaseRevision: revision, CreatedAt: now, Start: &s, Shortened: scale < 1, Changes: changes}, nil
}

// NextSnooze は、次の課題をスヌーズした場合の案。作れなければ nil。
func NextSnooze(tasks []Task, snoozeMinutes int, now time.Time, revision int64) *Draft {
	pending := Pending(tasks)
	if len(pending) == 0 {
		return nil
	}
	first := pending[0]
	snooze := time.Duration(snoozeMinutes) * time.Minute
	start := first.Plan.Start.Add(snooze)
	if start.Before(now) {
		start = now.Add(snooze)
	}
	d, err := MakeDraft(tasks, first.ID, DraftSnooze, start, now, revision)
	if err != nil {
		return nil
	}
	return &d
}

// EditDraft は課題の内容を変えた場合の案。対象の手順は捨て、未開始の課題を締切から組み直す。
// 組み直した後の課題の一覧も返す（保存するときの検証に使う）。
func EditDraft(tasks []Task, taskID string, in Input, now time.Time, revision int64) (Draft, []Task, error) {
	original, ok := Find(tasks, taskID)
	if !ok {
		return Draft{}, nil, apperr.NotFound("task.not_found")
	}
	if original.Status != StatusPending {
		return Draft{}, nil, apperr.Conflict("task.not_editable")
	}
	preview := slices.Clone(tasks)
	target, _ := Find(preview, taskID)
	target.ApplyInput(in)
	target.Plan = Plan{}
	Replan(preview)
	var changes []Change
	for _, after := range preview {
		if after.Status != StatusPending {
			continue
		}
		before, _ := Find(tasks, after.ID)
		plan := after.Plan
		plan.Reason = ReasonEditReplanned
		changes = append(changes, Change{TaskID: after.ID, Title: after.Title, Before: before.Plan, After: plan, OriginalMinutes: before.Minutes, PlannedSeconds: plan.Seconds()})
	}
	slices.SortStableFunc(changes, func(a, b Change) int { return a.After.Start.Compare(b.After.Start) })
	inputs := make([]ChangeInput, len(changes))
	for i, c := range changes {
		inputs[i] = ChangeInput{TaskID: c.TaskID, After: c.After}
	}
	if err := ValidateChanges(inputs, preview, now); err != nil {
		return Draft{}, nil, err
	}
	extra := fmt.Sprintf("%s|%s|%s|%d|%d|%s", in.Title, in.Notes, in.Why, in.Deadline.Unix(), in.Minutes, in.URL)
	return Draft{ID: draftID(DraftEdit, taskID, revision, now, extra), Kind: DraftEdit, TaskID: taskID, BaseRevision: revision, CreatedAt: now, Changes: changes}, preview, nil
}

// ValidateChanges は確定する変更を確かめる。変更は時刻の順に並び、重ならず、締切を超えず、手順が隙間なく並んでいること。
// 変更しない課題（未開始・作業中）とも重ならないこと。
func ValidateChanges(changes []ChangeInput, tasks []Task, now time.Time) error {
	if len(changes) == 0 {
		return apperr.Invalid("plan.invalid_changes", apperr.Field("changes", "plan.empty"))
	}
	invalid := func(i int, field, code string) error {
		path := fmt.Sprintf("changes/%d", i)
		if field != "" {
			path += "/" + field
		}
		return apperr.Invalid("plan.invalid_changes", apperr.Field(path, code))
	}
	seen := map[string]bool{}
	var last time.Time
	for i, c := range changes {
		t, ok := Find(tasks, c.TaskID)
		if !ok {
			return apperr.NotFound("task.not_found")
		}
		if seen[t.ID] || t.Status == StatusCompleted {
			return invalid(i, "taskId", "plan.duplicate_task")
		}
		seen[t.ID] = true
		p := c.After
		if !p.Reason.Valid() || utf8.RuneCountInString(p.Note) > MaxNoteRunes {
			return invalid(i, "after", "invalid_format")
		}
		if p.End.Sub(p.Start) < MinPlanSeconds*time.Second || p.Start.Before(now.Add(-time.Minute)) {
			return invalid(i, "after", "plan.invalid_time")
		}
		if p.End.After(t.Deadline) {
			return invalid(i, "after/end", "plan.exceeds_deadline")
		}
		if !last.IsZero() && p.Start.Before(last) {
			return invalid(i, "after/start", "plan.overlap")
		}
		last = p.End
		if len(p.Steps) == 0 {
			return invalid(i, "after/steps", "plan.steps_missing")
		}
		if len(p.Steps) > MaxSteps {
			return invalid(i, "after/steps", "plan.steps_invalid")
		}
		cursor := p.Start
		for _, step := range p.Steps {
			if strings.TrimSpace(step.Title) == "" || utf8.RuneCountInString(step.Title) > MaxStepTitleRunes ||
				!step.Start.Equal(cursor) || !step.End.After(step.Start) || step.End.After(p.End) {
				return invalid(i, "after/steps", "plan.steps_invalid")
			}
			cursor = step.End
		}
		if !cursor.Equal(p.End) {
			return invalid(i, "after/steps", "plan.end_mismatch")
		}
	}
	for i, c := range changes {
		for _, t := range tasks {
			if seen[t.ID] || t.Status == StatusCompleted {
				continue
			}
			if c.After.Start.Before(t.Plan.End) && t.Plan.Start.Before(c.After.End) {
				return invalid(i, "after", "plan.overlaps_other")
			}
		}
	}
	return nil
}

// Commit は検証済みの変更を課題に反映する。本人が選んだ順番と時刻をそのまま保存し、組み直さない。
func Commit(tasks []Task, changes []ChangeInput) []string {
	ids := make([]string, 0, len(changes))
	for _, c := range changes {
		t, _ := Find(tasks, c.TaskID)
		p := c.After
		p.Start, p.End = p.Start.UTC(), p.End.UTC()
		for i := range p.Steps {
			p.Steps[i].Start, p.Steps[i].End = p.Steps[i].Start.UTC(), p.Steps[i].End.UTC()
			p.Steps[i].Title = strings.TrimSpace(p.Steps[i].Title)
		}
		t.Plan = p
		ids = append(ids, t.ID)
	}
	return ids
}

// sampleTitles はデモのサンプルの課題の題名（本人のデータとして保存するので、本人の言語で作る）。
var sampleTitles = map[locale.Locale][3]string{
	locale.Ja: {"レポートの構成をまとめる", "演習問題を解く", "見直して提出する"},
	locale.En: {"Outline the report", "Work through the exercises", "Review and submit"},
}

// SampleInputs はデモのサンプルの課題。
func SampleInputs(now time.Time, lang locale.Locale) []Input {
	deadline := now.Add(90 * time.Minute)
	titles := sampleTitles[lang]
	return []Input{
		{Title: titles[0], Deadline: deadline, Minutes: 30},
		{Title: titles[1], Deadline: deadline, Minutes: 30},
		{Title: titles[2], Deadline: deadline, Minutes: 15},
	}
}
