// Package assistant は AI 相談（§2.13）を扱う。
//
// 相談は users/{uid}/assistantTurns/{id} に、処理中の印（リース）は users/{uid}/state/assistantLease に保存する。
// AI の返答に付くカードは提案だけで、何も保存しない。本人が押したときに、それぞれのエンドポイントで確定する。
package assistant

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// 相談の上限。
const (
	MaxTextRunes  = 2000
	MaxReplyRunes = 2000
	MaxCards      = 3
	// MaxRounds は1回の相談で AI を呼ぶ回数の上限（道具を使うたびに1回）。
	MaxRounds = 4
	// HistoryLimit は表示し、提案の修正（proposalId）を探す相談の数。
	HistoryLimit = 20
	// ContextTurns は AI に渡す過去の相談の数。
	ContextTurns = 4
	// MaxCardNotesRunes は予定カードのメモの上限。
	MaxCardNotesRunes = 1000
	// LeaseDuration は1つの相談を処理している間、同じ本人の次の相談を待たせる時間。
	LeaseDuration = 90 * time.Second
)

// Status は相談の状態。
type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusError   Status = "error"
)

// CardKind はカードの種類。
type CardKind string

const (
	CardCreate     CardKind = "create"
	CardReschedule CardKind = "reschedule"
	CardUpdate     CardKind = "update"
	CardMemory     CardKind = "memory"
	CardSleep      CardKind = "sleep"
)

// Card は返答に付く提案。種類ごとに使う項目が違う。
//   - create: InputID（登録するときの課題の ID）、Input、Preview、Revision
//   - reschedule: TaskID、Start（任意）、Preview（Start があるとき）、Revision
//   - update: TaskID、Input、Draft、Revision
//   - memory: Memory
//   - sleep: Sleep
//
// Revision は計画の版（登録・確定するときの If-Match）。
type Card struct {
	ID       string          `json:"id"`
	Kind     CardKind        `json:"kind"`
	InputID  string          `json:"inputId,omitempty"`
	Input    *task.Input     `json:"input,omitempty"`
	TaskID   string          `json:"taskId,omitempty"`
	Start    *time.Time      `json:"start,omitempty"`
	Preview  *task.Plan      `json:"preview,omitempty"`
	Draft    *task.Draft     `json:"draft,omitempty"`
	Revision int64           `json:"revision"`
	Memory   string          `json:"memory,omitempty"`
	Sleep    *sleep.Schedule `json:"sleep,omitempty"`
}

// Turn は1回の相談と、その返答。Failure は失敗したときの問題のコード（ProblemCode）。
type Turn struct {
	ID           string
	Text         string
	TaskID       string
	Status       Status
	Failure      string
	Reply        string
	Cards        []Card
	CreatedAt    time.Time
	PendingUntil *time.Time
}

// Lease は本人の相談を処理している印。RunID が一致する処理だけが結果を保存できる。
type Lease struct {
	RunID string    `json:"runId" firestore:"runId"`
	Until time.Time `json:"until" firestore:"until"`
}

// Active は now の時点で処理中か。
func (l Lease) Active(now time.Time) bool { return l.RunID != "" && now.Before(l.Until) }

// ValidateText は相談の文を整えて確かめる。
func ValidateText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", apperr.Invalid("request.invalid", apperr.Field("text", "required"))
	}
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return "", apperr.Invalid("request.invalid", apperr.Field("text", "too_long"))
	}
	return text, nil
}

// Begin は相談を始めてよいかを決める。run が false なら existing（返答済み）をそのまま返す。
// 同じ ID で内容が違えば assistant.turn_conflict、ほかの相談を処理中なら assistant.busy。
// 失敗した相談を同じ ID でやり直すときは、最初の作成時刻を残す。
func Begin(existing *Turn, lease Lease, id, text, taskID string, now time.Time) (turn Turn, run bool, err error) {
	if existing != nil {
		if existing.Text != text || existing.TaskID != taskID {
			return Turn{}, false, apperr.Conflict("assistant.turn_conflict")
		}
		if existing.Status == StatusReady {
			return *existing, false, nil
		}
	}
	if lease.Active(now) {
		return Turn{}, false, apperr.Conflict("assistant.busy")
	}
	created := now
	if existing != nil && !existing.CreatedAt.IsZero() {
		created = existing.CreatedAt
	}
	until := now.Add(LeaseDuration)
	return Turn{ID: id, Text: text, TaskID: taskID, Status: StatusPending, Cards: []Card{}, CreatedAt: created, PendingUntil: &until}, true, nil
}

// Present は表示用に、時間内に終わらなかった処理中の相談を失敗として見せる（保存はしない）。
func Present(t Turn, now time.Time) Turn {
	if t.Status == StatusPending && t.PendingUntil != nil && now.After(*t.PendingUntil) {
		t.Status, t.Failure = StatusError, "ai.failed"
	}
	return t
}

// Fail は相談を失敗にする（返答とカードは捨てる）。
func Fail(t Turn, code string) Turn {
	t.Status, t.Failure, t.Reply, t.Cards, t.PendingUntil = StatusError, code, "", []Card{}, nil
	return t
}

// Succeed は相談に返答を付ける。
func Succeed(t Turn, reply string, cards []Card) Turn {
	t.Status, t.Failure, t.Reply, t.Cards, t.PendingUntil = StatusReady, "", reply, cards, nil
	return t
}

// FailureCode はエラーを相談の失敗のコードにする。AI の出力が入力の検証に通らなかったものは ai.unreadable にする。
func FailureCode(err error) string {
	code := apperr.As(err).Code
	if code == "request.invalid" {
		return "ai.unreadable"
	}
	return code
}

// 道具の名前。get_* は読むだけ、propose_* はカードになる。
const (
	ToolGetToday        = "get_today"
	ToolGetProgress     = "get_progress"
	ToolGetCalendar     = "get_calendar"
	ToolGetProfile      = "get_profile"
	ToolProposeTask     = "propose_task"
	ToolProposeSchedule = "propose_reschedule"
	ToolProposeUpdate   = "propose_update"
	ToolProposeMemory   = "propose_memory"
	ToolProposeSleep    = "propose_sleep"
)

// proposalKind は提案の道具が作るカードの種類。提案の道具でなければ ok は false。
func proposalKind(name string) (CardKind, bool) {
	switch name {
	case ToolProposeTask:
		return CardCreate, true
	case ToolProposeSchedule:
		return CardReschedule, true
	case ToolProposeUpdate:
		return CardUpdate, true
	case ToolProposeMemory:
		return CardMemory, true
	case ToolProposeSleep:
		return CardSleep, true
	default:
		return "", false
	}
}

// IsProposal は提案の道具か。
func IsProposal(name string) bool {
	_, ok := proposalKind(name)
	return ok
}

// ToolCall は AI が呼んだ道具。Args は信頼できない入力。
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// Step は AI の1回の応答。
type Step struct {
	Reply string
	Calls []ToolCall
}

// ToolResult は道具の結果。Proposal なら「本人の確認待ち（未保存）」として AI に返す。
type ToolResult struct {
	ID       string
	Name     string
	Proposal bool
	Data     any
}

// Proposal は提案の道具の引数を読み取ったもの。まだ検証していない。
type Proposal struct {
	Kind       CardKind
	Input      *task.Input
	ProposalID string
	TaskID     string
	Start      *time.Time
	Memory     string
	Sleep      *sleep.Schedule
	// Omitted は update で AI が省いた任意の項目（notes・why・url）。課題の今の値を使う。
	Omitted map[string]bool
}

type proposalArgs struct {
	Title      string `json:"title"`
	Notes      string `json:"notes"`
	Why        string `json:"why"`
	Deadline   string `json:"deadline"`
	Minutes    int    `json:"minutes"`
	URL        string `json:"url"`
	ProposalID string `json:"proposalId"`
	TaskID     string `json:"taskId"`
	Start      string `json:"start"`
	Memory     string `json:"memory"`
	Bedtime    string `json:"bedtime"`
	Wake       string `json:"wake"`
}

func unreadable() error { return apperr.Unavailable("ai.unreadable") }

func parseTime(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, unreadable()
	}
	return &t, nil
}

// ParseProposal は提案の道具の引数を読み取る。形が違えば ai.unreadable。
func ParseProposal(call ToolCall) (Proposal, error) {
	kind, ok := proposalKind(call.Name)
	if !ok {
		return Proposal{}, unreadable()
	}
	raw, err := json.Marshal(call.Args)
	if err != nil {
		return Proposal{}, unreadable()
	}
	var args proposalArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Proposal{}, unreadable()
	}
	p := Proposal{Kind: kind, TaskID: strings.TrimSpace(args.TaskID), ProposalID: strings.TrimSpace(args.ProposalID)}
	switch kind {
	case CardCreate, CardUpdate:
		deadline, err := parseTime(args.Deadline)
		if err != nil {
			return Proposal{}, err
		}
		in := task.Input{Title: args.Title, Notes: args.Notes, Why: args.Why, Minutes: args.Minutes, URL: args.URL}
		if deadline != nil {
			in.Deadline = *deadline
		}
		p.Input = &in
		if kind == CardUpdate {
			p.Omitted = map[string]bool{}
			for _, field := range []string{"notes", "why", "url"} {
				if _, ok := call.Args[field]; !ok {
					p.Omitted[field] = true
				}
			}
		}
	case CardReschedule:
		if p.Start, err = parseTime(args.Start); err != nil {
			return Proposal{}, err
		}
	case CardMemory:
		p.Memory = args.Memory
	case CardSleep:
		p.Sleep = &sleep.Schedule{Bedtime: args.Bedtime, Wake: args.Wake}
	}
	return p, nil
}

// ValidateReply は AI の返答の文と提案の数を確かめる。
func ValidateReply(reply string, proposals int) (string, error) {
	reply = strings.TrimSpace(reply)
	if reply == "" || utf8.RuneCountInString(reply) > MaxReplyRunes || proposals > MaxCards {
		return "", unreadable()
	}
	return reply, nil
}

// CalendarInfo は get_calendar に渡すカレンダー連携の状態。
type CalendarInfo struct {
	Connected bool
	Sync      string
	LastSync  *time.Time
	// Linked はカレンダーの予定と結び付いている課題の ID。
	Linked map[string]bool
}

// Snapshot は相談の間に AI が道具で読む本人のデータ。相談を始める時点で一度だけ読む。
type Snapshot struct {
	Now       time.Time
	Loc       *time.Location
	Profile   profile.Profile
	Tasks     []task.Task
	Revision  int64
	Account   wallet.Account
	Sleep     companion.SleepSummary
	WorkingOn string
	AppOpens  *companion.OpenCounts
	Calendar  CalendarInfo
}

type todayResult struct {
	Now                string                  `json:"now"`
	Tasks              []companion.TaskSummary `json:"tasks"`
	TimePolicy         companion.TimePolicy    `json:"timePolicy"`
	ViewingMinutesLeft int                     `json:"viewingMinutesLeft"`
	Sleep              companion.SleepSummary  `json:"sleep"`
	WorkingOn          string                  `json:"workingOn,omitempty"`
}

type calendarResult struct {
	Connected bool                    `json:"connected"`
	Sync      string                  `json:"sync"`
	LastSync  *time.Time              `json:"lastSync,omitempty"`
	Events    []companion.TaskSummary `json:"events"`
}

type profileResult struct {
	Name     string `json:"name"`
	Context  string `json:"context"`
	TimeZone string `json:"timeZone"`
}

type toolError struct {
	Error string `json:"error"`
}

// ReadTool は読むだけの道具の結果を返す。知らない道具なら error を返す（AI に伝えるだけで、相談は続ける）。
func ReadTool(name string, s Snapshot) any {
	describe := func(t task.Task) companion.TaskSummary { return companion.Describe(t, s.Calendar.Linked[t.ID], s.Loc) }
	switch name {
	case ToolGetToday:
		y, m, d := s.Now.In(s.Loc).Date()
		until := time.Date(y, m, d, 0, 0, 0, 0, s.Loc).AddDate(0, 0, 2)
		r := todayResult{
			Now: s.Now.In(s.Loc).Format(time.RFC3339), Tasks: []companion.TaskSummary{}, TimePolicy: companion.PolicyOf(s.Account),
			ViewingMinutesLeft: s.Account.Available() / 60, Sleep: s.Sleep, WorkingOn: s.WorkingOn,
		}
		for _, t := range s.Tasks {
			if t.Status != task.StatusCompleted && t.Plan.Start.Before(until) {
				r.Tasks = append(r.Tasks, describe(t))
			}
		}
		return r
	case ToolGetProgress:
		return companion.BuildProgress(companion.ProgressInput{Tasks: s.Tasks, Account: s.Account, Sleep: s.Sleep, AppOpens: s.AppOpens, Now: s.Now, Loc: s.Loc})
	case ToolGetCalendar:
		r := calendarResult{Connected: s.Calendar.Connected, Sync: s.Calendar.Sync, LastSync: s.Calendar.LastSync, Events: []companion.TaskSummary{}}
		for _, t := range s.Tasks {
			if s.Calendar.Linked[t.ID] && t.Status != task.StatusCompleted {
				r.Events = append(r.Events, describe(t))
			}
		}
		return r
	case ToolGetProfile:
		return profileResult{Name: s.Profile.Name, Context: s.Profile.Context, TimeZone: s.Profile.TimeZone}
	default:
		return toolError{Error: "unknown_tool"}
	}
}

// ConsultInput は相談のために AI に渡す内容（本人が入力した相談と、過去の相談・選んだ課題）。
type ConsultInput struct {
	// Locale は返答の言語。AI への指示に使い、入力の JSON には入れない。
	Locale       locale.Locale `json:"-"`
	TimeZone     string        `json:"timeZone"`
	History      []HistoryTurn `json:"history"`
	SelectedTask *SelectedTask `json:"selectedTask"`
	User         string        `json:"user"`
	Now          string        `json:"now"`
}

// HistoryTurn は AI に渡す過去の相談。
type HistoryTurn struct {
	User      string        `json:"user"`
	Assistant string        `json:"assistant"`
	Cards     []HistoryCard `json:"cards"`
	TaskID    string        `json:"taskId,omitempty"`
}

// HistoryCard は過去のカード。AI が参照・修正するのに要る項目だけで、計画のプレビューは含めない。
type HistoryCard struct {
	ID     string          `json:"id"`
	Kind   CardKind        `json:"kind"`
	Input  *HistoryInput   `json:"input,omitempty"`
	TaskID string          `json:"taskId,omitempty"`
	Memory string          `json:"memory,omitempty"`
	Sleep  *sleep.Schedule `json:"sleep,omitempty"`
}

// HistoryInput は過去のカードの課題の内容。
type HistoryInput struct {
	Title    string    `json:"title"`
	Notes    string    `json:"notes"`
	Why      string    `json:"why"`
	Deadline time.Time `json:"deadline"`
	Minutes  int       `json:"minutes"`
	URL      string    `json:"url"`
}

// SelectedTask は本人が選んで相談している課題。
type SelectedTask struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Deadline time.Time   `json:"deadline"`
	Minutes  int         `json:"minutes"`
	Plan     task.Plan   `json:"plan"`
	Notes    string      `json:"notes"`
	Why      string      `json:"why"`
	URL      string      `json:"url"`
	Status   task.Status `json:"status"`
}

// Select は選んだ課題を AI に渡す形にする。
func Select(t task.Task, loc *time.Location) *SelectedTask {
	return &SelectedTask{ID: t.ID, Title: t.Title, Deadline: t.Deadline.In(loc), Minutes: t.Minutes, Plan: t.Plan, Notes: t.Notes, Why: t.Why, URL: t.URL, Status: t.Status}
}

// HistoryCards は過去のカードから、プレビューを除いた内容を作る。
func HistoryCards(cards []Card) []HistoryCard {
	out := make([]HistoryCard, 0, len(cards))
	for _, c := range cards {
		h := HistoryCard{ID: c.ID, Kind: c.Kind, TaskID: c.TaskID, Memory: c.Memory, Sleep: c.Sleep}
		if c.Input != nil {
			h.Input = &HistoryInput{Title: c.Input.Title, Notes: c.Input.Notes, Why: c.Input.Why, Deadline: c.Input.Deadline, Minutes: c.Input.Minutes, URL: c.Input.URL}
		}
		out = append(out, h)
	}
	return out
}

// History は AI に渡す過去の相談。返答済みで、作業傾向を書き換えた時刻（since）以降のものを、新しい順に最大4件、古い順で返す。
// turns は古い順。exclude（今の相談）は含めない。
func History(turns []Turn, since *time.Time, exclude string) []HistoryTurn {
	out := []HistoryTurn{}
	for _, t := range turns {
		if t.ID == exclude || t.Status != StatusReady || (since != nil && t.CreatedAt.Before(*since)) {
			continue
		}
		out = append(out, HistoryTurn{User: t.Text, Assistant: t.Reply, Cards: HistoryCards(t.Cards), TaskID: t.TaskID})
	}
	if len(out) > ContextTurns {
		out = out[len(out)-ContextTurns:]
	}
	return out
}

// BuildCard は提案を検証し、プレビューを付けたカードにする。何も保存しない。
// history は proposalId（以前の未登録のカード）を探す範囲。
func BuildCard(p Proposal, s Snapshot, history []Turn, cardID string) (Card, error) {
	card := Card{ID: cardID, Kind: p.Kind, Revision: s.Revision}
	switch p.Kind {
	case CardCreate:
		return createCard(card, p, s, history)
	case CardReschedule:
		t, ok := task.Find(s.Tasks, p.TaskID)
		if !ok {
			return Card{}, apperr.NotFound("task.not_found")
		}
		card.TaskID = t.ID
		if p.Start != nil {
			d, err := task.MakeDraft(s.Tasks, t.ID, task.DraftAdjust, *p.Start, s.Now, s.Revision)
			if err != nil {
				return Card{}, err
			}
			start, after := d.Changes[0].After.Start, d.Changes[0].After
			card.Start, card.Preview = &start, &after
		}
		return card, nil
	case CardUpdate:
		t, ok := task.Find(s.Tasks, p.TaskID)
		if !ok {
			return Card{}, apperr.NotFound("task.not_found")
		}
		if p.Input == nil {
			return Card{}, unreadable()
		}
		in := *p.Input
		if p.Omitted["notes"] {
			in.Notes = t.Notes
		}
		if p.Omitted["why"] {
			in.Why = t.Why
		}
		if p.Omitted["url"] {
			in.URL = t.URL
		}
		if err := task.ValidateInput(in, s.Now); err != nil {
			return Card{}, err
		}
		d, _, err := task.EditDraft(s.Tasks, t.ID, in, s.Now, s.Revision)
		if err != nil {
			return Card{}, err
		}
		card.TaskID, card.Input, card.Draft = t.ID, &in, &d
		return card, nil
	case CardMemory:
		memory := strings.TrimSpace(p.Memory)
		if memory == "" || utf8.RuneCountInString(memory) > profile.MaxMemoryRunes {
			return Card{}, unreadable()
		}
		card.Memory = memory
		return card, nil
	case CardSleep:
		if p.Sleep == nil || p.Sleep.Validate() != nil {
			return Card{}, unreadable()
		}
		card.Sleep = p.Sleep
		return card, nil
	default:
		return Card{}, unreadable()
	}
}

// createCard は新しい課題のカード。proposalId があれば、以前のカードの課題の ID を引き継ぐ（修正しても別の課題にしない）。
func createCard(card Card, p Proposal, s Snapshot, history []Turn) (Card, error) {
	if p.Input == nil || utf8.RuneCountInString(p.Input.Notes) > MaxCardNotesRunes {
		return Card{}, unreadable()
	}
	inputID := card.ID
	if p.ProposalID != "" {
		found := false
		for _, t := range history {
			for _, old := range t.Cards {
				if old.ID == p.ProposalID && old.Kind == CardCreate && old.InputID != "" {
					inputID, found = old.InputID, true
				}
			}
		}
		if !found {
			return Card{}, unreadable()
		}
	}
	if err := task.ValidateInput(*p.Input, s.Now); err != nil {
		return Card{}, err
	}
	if _, exists := task.Find(s.Tasks, inputID); exists {
		return Card{}, apperr.Conflict("task.duplicate")
	}
	created := task.NewTask(inputID, *p.Input, s.Now)
	in := task.InputOf(created)
	preview := append(slices.Clone(s.Tasks), created)
	task.Replan(preview)
	t, _ := task.Find(preview, inputID)
	plan := t.Plan
	card.InputID, card.Input, card.Preview = inputID, &in, &plan
	return card, nil
}
