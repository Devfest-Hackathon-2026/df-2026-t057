package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/worksession"
)

type noCalendar struct{}

func (noCalendar) TaskContentChanged(context.Context, *store.Tx, string, string) error { return nil }
func (noCalendar) PlansChanged(context.Context, *store.Tx, string) error               { return nil }
func (noCalendar) TaskRemoved(context.Context, *store.Tx, string, string) error        { return nil }
func (noCalendar) LinkedTasks(context.Context, *store.Tx, string) (map[string]bool, error) {
	return nil, nil
}
func (noCalendar) ProfileFormatChanged(context.Context, *store.Tx, string) error { return nil }
func (noCalendar) SleepChanged(context.Context, *store.Tx, string) error         { return nil }
func (noCalendar) SleepCleared(context.Context, *store.Tx, string) error         { return nil }
func (noCalendar) SleepPending(context.Context, *store.Tx, string) (bool, error) {
	return false, nil
}

type noPlanner struct{}

func (noPlanner) Detail(context.Context, string, task.DetailRequest) (task.DetailResult, error) {
	return task.DetailResult{}, apperr.Unavailable("ai.unavailable")
}
func (noPlanner) Extract(context.Context, string, string, time.Time, *time.Location) ([]task.Input, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}
func (noPlanner) Adjust(context.Context, string, task.AdjustRequest) ([]task.ChangeInput, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}

type linkedCalendar struct{ linked map[string]bool }

func (c linkedCalendar) Status(context.Context, *store.Tx, string) (CalendarInfo, error) {
	return CalendarInfo{Connected: true, Sync: "synced", Linked: c.linked}, nil
}

// script は AI の応答を決める。round は1から数える。
type script func(in ConsultInput, round int, results []ToolResult) (Step, error)

type fakeModel struct {
	mu      sync.Mutex
	script  script
	calls   int
	inputs  []ConsultInput
	results [][]ToolResult
}

func (m *fakeModel) Consult(in ConsultInput) Conversation {
	return &fakeConversation{model: m, input: in}
}

func (m *fakeModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *fakeModel) lastInput() ConsultInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inputs[len(m.inputs)-1]
}

type fakeConversation struct {
	model *fakeModel
	input ConsultInput
	round int
}

func (c *fakeConversation) Next(_ context.Context, _ string, results []ToolResult) (Step, error) {
	c.model.mu.Lock()
	c.model.calls++
	c.round++
	c.model.inputs = append(c.model.inputs, c.input)
	c.model.results = append(c.model.results, results)
	s := c.model.script
	c.model.mu.Unlock()
	return s(c.input, c.round, results)
}

func reply(text string, calls ...ToolCall) script {
	return func(ConsultInput, int, []ToolResult) (Step, error) { return Step{Reply: text, Calls: calls}, nil }
}

func call(name string, args map[string]any) ToolCall { return ToolCall{Name: name, Args: args} }

type world struct {
	runner   *store.Runner
	clock    *clock.Fake
	tasks    *task.Service
	profiles *profile.Service
	model    *fakeModel
	svc      *Service
}

func newWorld(t *testing.T, s script) *world {
	t.Helper()
	runner := store.NewRunner(memory.NewStore())
	c := clock.NewFake(time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC))
	profiles := profile.New(runner, c, noCalendar{})
	wallets := wallet.New(runner, c, profiles, sleep.NewNights(profiles), worksession.Sessions{})
	sleeps := sleep.New(runner, c, profiles, wallets, noCalendar{})
	tasks := task.New(runner, c, &id.Sequence{Prefix: "t"}, task.Deps{
		Planner: noPlanner{}, Calendar: noCalendar{}, Sessions: worksession.Sessions{}, Profiles: profiles, Rewards: wallets,
	})
	model := &fakeModel{script: s}
	svc := New(runner, c, &id.Sequence{Prefix: "c"}, Deps{
		Tasks: tasks, Profiles: profiles, Wallet: wallets, Sleep: sleeps, Sessions: worksession.Sessions{}, Model: model,
	})
	return &world{runner: runner, clock: c, tasks: tasks, profiles: profiles, model: model, svc: svc}
}

func (w *world) create(t *testing.T, uid, taskID string, in task.Input) task.Task {
	t.Helper()
	if in.Title == "" {
		in.Title = "レポート"
	}
	if in.Deadline.IsZero() {
		in.Deadline = w.clock.Now().Add(4 * time.Hour)
	}
	if in.Minutes == 0 {
		in.Minutes = 30
	}
	created, err := w.tasks.Create(t.Context(), uid, task.CreateRequest{ID: taskID, Input: in})
	if err != nil {
		t.Fatal(err)
	}
	return created.Task
}

func (w *world) send(t *testing.T, uid, turnID, text, taskID string) Turn {
	t.Helper()
	turn, err := w.svc.Send(t.Context(), uid, turnID, text, taskID)
	if err != nil {
		t.Fatalf("send %s: %v", turnID, err)
	}
	return turn
}

func (w *world) deadline(hours int) string {
	return w.clock.Now().Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)
}

func TestCardsRequireConfirmationAndReplayDoesNotCallModel(t *testing.T) {
	w := newWorld(t, nil)
	w.model.script = reply("登録前の候補を用意したよ。",
		call(ToolProposeTask, map[string]any{"id": "model-id", "title": "レポート", "minutes": 30, "deadline": w.deadline(4)}),
		call(ToolProposeMemory, map[string]any{"memory": " 短い作業に区切ると進めやすい "}),
	)
	turn := w.send(t, "u", "first", " 4時間後までにレポート30分 ", "")
	if turn.Status != StatusReady || turn.Text != "4時間後までにレポート30分" || len(turn.Cards) != 2 {
		t.Fatalf("turn: %+v", turn)
	}
	create, memo := turn.Cards[0], turn.Cards[1]
	if create.Kind != CardCreate || create.InputID != create.ID || create.InputID == "model-id" || create.Preview == nil || create.Preview.End.IsZero() || create.Revision != 0 {
		t.Fatalf("create card: %+v", create)
	}
	if memo.Kind != CardMemory || memo.Memory != "短い作業に区切ると進めやすい" {
		t.Fatalf("memory card: %+v", memo)
	}
	list, _ := w.tasks.List(t.Context(), "u")
	p, _, _ := w.profiles.Get(t.Context(), "u")
	if len(list.Items) != 0 || p.Context != "" {
		t.Fatal("a card saved something without confirmation")
	}
	repeated := w.send(t, "u", "first", "4時間後までにレポート30分", "")
	if repeated.Cards[0].ID != create.ID || w.model.callCount() != 1 {
		t.Fatal("a completed request was generated twice")
	}
	if _, err := w.svc.Send(t.Context(), "u", "first", "別の内容", ""); apperr.As(err).Code != "assistant.turn_conflict" {
		t.Fatalf("request id reuse: %v", err)
	}
	// カードを押して登録すると、カードの ID と版で1件だけ登録される。
	rev := create.Revision
	if _, err := w.tasks.Create(t.Context(), "u", task.CreateRequest{ID: create.InputID, Input: *create.Input, IfMatch: &rev}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tasks.Create(t.Context(), "u", task.CreateRequest{ID: create.InputID, Input: *create.Input}); apperr.As(err).Code != "task.duplicate" {
		t.Fatalf("second registration: %v", err)
	}
	if _, err := w.tasks.Create(t.Context(), "u", task.CreateRequest{ID: "stale", Input: *create.Input, IfMatch: &rev}); apperr.As(err).Code != "precondition.failed" {
		t.Fatalf("stale card revision: %v", err)
	}
	other, err := w.svc.List(t.Context(), "someone-else")
	if err != nil || len(other) != 0 {
		t.Fatal("another user's conversation was returned")
	}
}

func TestSendValidatesTextAndAvailability(t *testing.T) {
	w := newWorld(t, reply("ok"))
	for text, code := range map[string]string{"  ": "required", strings.Repeat("あ", MaxTextRunes+1): "too_long"} {
		_, err := w.svc.Send(t.Context(), "u", "x", text, "")
		if e := apperr.As(err); e.Code != "request.invalid" || e.Fields[0].Field != "text" || e.Fields[0].Code != code {
			t.Fatalf("%q: %v", code, err)
		}
	}
	if _, err := w.svc.Send(t.Context(), "u", "x", strings.Repeat("あ", MaxTextRunes), ""); err != nil {
		t.Fatalf("max length: %v", err)
	}
	w.svc.Model = nil
	if _, err := w.svc.Send(t.Context(), "u", "y", "相談", ""); apperr.As(err).Code != "ai.unavailable" {
		t.Fatalf("demo: %v", err)
	}
	if turns, _ := w.svc.List(t.Context(), "u"); len(turns) != 1 {
		t.Fatal("an unavailable consultation was saved")
	}
}

func TestForgottenContextIsNotSentButStillShown(t *testing.T) {
	w := newWorld(t, reply("作業の傾向を確認したよ。"))
	w.send(t, "u", "before", "忘れる目印という作業の好みがあります", "")
	w.clock.Advance(time.Second)
	if _, _, err := w.profiles.Remember(t.Context(), "u", "忘れる目印"); err != nil {
		t.Fatal(err)
	}
	w.send(t, "u", "middle", "覚えた？", "")
	if h := w.model.lastInput().History; len(h) != 1 || h[0].User != "忘れる目印という作業の好みがあります" {
		t.Fatalf("remembering must not forget history: %+v", h)
	}
	w.clock.Advance(time.Minute)
	_, rev, _ := w.profiles.Get(t.Context(), "u")
	if _, _, err := w.profiles.Update(t.Context(), "u", profile.Input{Name: "本人", TimeZone: "Asia/Tokyo", Locale: locale.Ja}, rev); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(time.Minute)
	turn := w.send(t, "u", "after", "今の作業の傾向を教えて", "")
	if turn.Status != StatusReady || len(w.model.lastInput().History) != 0 {
		t.Fatalf("removed context was resent: %+v", w.model.lastInput().History)
	}
	turns, _ := w.svc.List(t.Context(), "u")
	if len(turns) != 3 || turns[0].Text != "忘れる目印という作業の好みがあります" || turns[2].ID != "after" {
		t.Fatal("forgetting deleted the visible conversation")
	}
}

func TestHistoryKeepsLastFourReadyTurns(t *testing.T) {
	w := newWorld(t, reply("ok"))
	for i := range 6 {
		w.send(t, "u", fmt.Sprintf("t%d", i), fmt.Sprintf("相談%d", i), "")
		w.clock.Advance(time.Second)
	}
	h := w.model.lastInput().History
	if len(h) != ContextTurns || h[0].User != "相談1" || h[3].User != "相談4" {
		t.Fatalf("history: %+v", h)
	}
}

func TestUnknownTaskCardFailsTurnAndCanBeRetried(t *testing.T) {
	w := newWorld(t, nil)
	var calls atomic.Int32
	w.model.script = func(ConsultInput, int, []ToolResult) (Step, error) {
		if calls.Add(1) == 1 {
			return Step{Reply: "予定を確認するね。", Calls: []ToolCall{call(ToolProposeSchedule, map[string]any{"taskId": "someone-else"})}}, nil
		}
		return Step{Reply: "課題名と締切を教えて。"}, nil
	}
	turn := w.send(t, "u", "retry", "この予定を調整したい", "")
	if turn.Status != StatusError || turn.Failure != "task.not_found" || len(turn.Cards) != 0 || turn.Reply != "" {
		t.Fatalf("unknown task card: %+v", turn)
	}
	turn = w.send(t, "u", "retry", "この予定を調整したい", "")
	if turn.Status != StatusReady || calls.Load() != 2 {
		t.Fatal("the failed turn could not be retried")
	}
}

func TestLeaseAllowsOneWorkerAndHistoryKeepsTwenty(t *testing.T) {
	w := newWorld(t, reply("ok"))
	var winners atomic.Int32
	var runID string
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, run, err := w.svc.begin(t.Context(), "u", "request", "相談", "")
			switch {
			case err == nil && run != "":
				winners.Add(1)
				runID = run
			case apperr.As(err).Code != "assistant.busy":
				t.Errorf("begin: %v", err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("%d workers won the lease", winners.Load())
	}
	if _, err := w.svc.Send(t.Context(), "u", "another", "相談", ""); apperr.As(err).Code != "assistant.busy" {
		t.Fatalf("lease did not block another request: %v", err)
	}
	pending := Turn{ID: "request", Text: "相談", Status: StatusReady, Reply: "ok"}
	if _, err := w.svc.finish(t.Context(), "u", pending, "wrong-run", profile.Profile{}); apperr.As(err).Code != "assistant.turn_conflict" {
		t.Fatalf("wrong run released the lease: %v", err)
	}
	if _, err := w.svc.finish(t.Context(), "u", pending, runID, profile.Default()); err != nil {
		t.Fatal(err)
	}
	for i := range 24 {
		w.clock.Advance(time.Second)
		w.send(t, "u", fmt.Sprintf("turn-%d", i), "相談", "")
	}
	turns, err := w.svc.List(t.Context(), "u")
	if err != nil || len(turns) != HistoryLimit || turns[0].ID != "turn-4" || turns[HistoryLimit-1].ID != "turn-23" {
		t.Fatalf("history: %d %v", len(turns), err)
	}
}

func TestExpiredPendingTurnIsPresentedAsErrorAndRetryable(t *testing.T) {
	w := newWorld(t, reply("ok"))
	if _, _, err := w.svc.begin(t.Context(), "u", "stuck", "相談", ""); err != nil {
		t.Fatal(err)
	}
	if turns, _ := w.svc.List(t.Context(), "u"); turns[0].Status != StatusPending {
		t.Fatal("pending turn")
	}
	w.clock.Advance(LeaseDuration + time.Second)
	turns, _ := w.svc.List(t.Context(), "u")
	if turns[0].Status != StatusError || turns[0].Failure != "ai.failed" {
		t.Fatalf("expired pending: %+v", turns[0])
	}
	created := turns[0].CreatedAt
	turn := w.send(t, "u", "stuck", "相談", "")
	if turn.Status != StatusReady || !turn.CreatedAt.Equal(created) {
		t.Fatalf("retry: %+v", turn)
	}
}

func TestRevisingUnregisteredProposalKeepsInputID(t *testing.T) {
	w := newWorld(t, nil)
	w.model.script = reply("30分の候補だよ", call(ToolProposeTask, map[string]any{"title": "確認用", "minutes": 30, "deadline": w.deadline(4)}))
	first := w.send(t, "u", "original", "確認用を30分で", "")
	proposalID := first.Cards[0].ID
	w.model.script = func(in ConsultInput, _ int, _ []ToolResult) (Step, error) {
		if len(in.History) != 1 || in.History[0].Cards[0].ID != proposalID {
			t.Error("the revision did not receive the previous proposal")
		}
		return Step{Reply: "40分の候補だよ", Calls: []ToolCall{call(ToolProposeTask, map[string]any{"proposalId": proposalID, "title": "確認用", "minutes": 40, "deadline": w.deadline(4)})}}, nil
	}
	second := w.send(t, "u", "revised", "さっきの候補を40分に", "")
	if second.Status != StatusReady || len(second.Cards) != 1 {
		t.Fatalf("revision failed: %+v", second)
	}
	card := second.Cards[0]
	if card.ID == proposalID || card.InputID != first.Cards[0].InputID || card.Input.Minutes != 40 {
		t.Fatalf("the revision lost the stable id or minutes: %+v", card)
	}
	if list, _ := w.tasks.List(t.Context(), "u"); len(list.Items) != 0 {
		t.Fatal("revising a proposal registered a task")
	}
	// 登録した後に同じ候補を直そうとすると、複製しない。
	if _, err := w.tasks.Create(t.Context(), "u", task.CreateRequest{ID: card.InputID, Input: *card.Input}); err != nil {
		t.Fatal(err)
	}
	w.model.script = reply("50分にするね", call(ToolProposeTask, map[string]any{"proposalId": proposalID, "title": "確認用", "minutes": 50, "deadline": w.deadline(4)}))
	if turn := w.send(t, "u", "after-register", "50分に", ""); turn.Status != StatusError || turn.Failure != "task.duplicate" {
		t.Fatalf("registered proposal was duplicated: %+v", turn)
	}
	w.model.script = reply("直すね", call(ToolProposeTask, map[string]any{"proposalId": "missing", "title": "確認用", "minutes": 50, "deadline": w.deadline(4)}))
	if turn := w.send(t, "u", "unknown-proposal", "直して", ""); turn.Failure != "ai.unreadable" {
		t.Fatalf("unknown proposal: %+v", turn)
	}
}

func TestSelectedTaskIsTheOwnedTaskByID(t *testing.T) {
	w := newWorld(t, nil)
	w.create(t, "u", "selected-a", task.Input{Title: "同じ名前の課題"})
	w.create(t, "u", "selected-b", task.Input{Title: "同じ名前の課題"})
	w.model.script = func(in ConsultInput, _ int, _ []ToolResult) (Step, error) {
		if in.SelectedTask == nil || in.SelectedTask.ID != "selected-b" || in.SelectedTask.Title != "同じ名前の課題" {
			t.Errorf("wrong selection: %+v", in.SelectedTask)
		}
		start := w.clock.Now().Add(time.Hour).Format(time.RFC3339)
		return Step{Reply: "選んだ予定だよ", Calls: []ToolCall{call(ToolProposeSchedule, map[string]any{"taskId": "selected-b", "start": start})}}, nil
	}
	turn := w.send(t, "u", "selection", "この予定を見直したい", "selected-b")
	if turn.Status != StatusReady || turn.TaskID != "selected-b" || turn.Cards[0].TaskID != "selected-b" || turn.Cards[0].Preview == nil ||
		turn.Cards[0].Start == nil || !turn.Cards[0].Start.Equal(w.clock.Now().Add(time.Hour)) || turn.Cards[0].Revision == 0 {
		t.Fatalf("selection: %+v", turn)
	}
	w.send(t, "u", "selection", "この予定を見直したい", "selected-b")
	if w.model.callCount() != 1 {
		t.Fatal("replay regenerated the response")
	}
	if _, err := w.svc.Send(t.Context(), "u", "selection", "この予定を見直したい", "selected-a"); apperr.As(err).Code != "assistant.turn_conflict" {
		t.Fatalf("same id with another task: %v", err)
	}
	foreign := w.send(t, "other", "foreign-selection", "この予定を見直したい", "selected-b")
	if foreign.Status != StatusError || foreign.Failure != "task.not_found" || w.model.callCount() != 1 {
		t.Fatal("a foreign task was sent to the model")
	}
}

func TestUpdateCardKeepsOmittedFieldsAndSavesNothing(t *testing.T) {
	w := newWorld(t, nil)
	existing := w.create(t, "u", "one", task.Input{Title: "レポート", Notes: "参考文献は3冊", Why: "単位を落とさない", URL: "https://example.com/report"})
	w.model.script = reply("45分にしてみたよ。", call(ToolProposeUpdate, map[string]any{"taskId": "one", "title": "レポート", "deadline": w.deadline(6), "minutes": 45}))
	before, _ := w.tasks.List(t.Context(), "u")
	turn := w.send(t, "u", "keep", "レポート45分にして", "")
	if turn.Status != StatusReady || len(turn.Cards) != 1 {
		t.Fatalf("update card: %+v", turn)
	}
	card := turn.Cards[0]
	if card.Kind != CardUpdate || card.TaskID != "one" || card.Draft == nil || card.Draft.Kind != task.DraftEdit || card.Revision != before.Revision {
		t.Fatalf("card: %+v", card)
	}
	if got := card.Input; got.Notes != existing.Notes || got.Why != existing.Why || got.URL != existing.URL || got.Minutes != 45 {
		t.Fatalf("omitted fields were cleared: %+v", got)
	}
	after, _ := w.tasks.List(t.Context(), "u")
	if after.Revision != before.Revision || after.Items[0].Minutes != 30 {
		t.Fatal("an update card saved the change")
	}
	w.model.script = reply("消すね", call(ToolProposeUpdate, map[string]any{"taskId": "one", "title": "レポート", "deadline": w.deadline(6), "minutes": 45, "notes": ""}))
	if turn := w.send(t, "u", "clear", "メモを消して", ""); turn.Cards[0].Input.Notes != "" {
		t.Fatal("an explicit empty note was replaced")
	}
	w.model.script = reply("直すね", call(ToolProposeUpdate, map[string]any{"taskId": "one", "title": "レポート", "deadline": w.deadline(6), "minutes": 45}))
	if foreign := w.send(t, "other", "foreign", "この課題を編集して", ""); foreign.Status != StatusError || foreign.Failure != "task.not_found" {
		t.Fatalf("a foreign task edit was proposed: %+v", foreign)
	}
}

func TestToolsReachModelAndProposalsMidLoopBecomeCards(t *testing.T) {
	w := newWorld(t, nil)
	w.create(t, "u", "seminar", task.Input{Title: "ゼミ発表", Why: "先生に認められたい"})
	w.svc.Calendar = linkedCalendar{linked: map[string]bool{"seminar": true}}
	w.model.script = func(_ ConsultInput, round int, results []ToolResult) (Step, error) {
		switch round {
		case 1:
			return Step{Calls: []ToolCall{{ID: "c1", Name: ToolGetToday, Args: map[string]any{}}, {ID: "c2", Name: ToolGetCalendar}, {ID: "c3", Name: "get_secrets"}}}, nil
		case 2:
			today, _ := results[0].Data.(todayResult)
			calendar, _ := results[1].Data.(calendarResult)
			if results[0].ID != "c1" || len(today.Tasks) != 1 || today.Tasks[0].Title != "ゼミ発表" || !today.Tasks[0].FromCalendar ||
				len(calendar.Events) != 1 || !calendar.Connected || results[2].Data != (toolError{Error: "unknown_tool"}) {
				t.Errorf("tool results: %+v", results)
			}
			return Step{Calls: []ToolCall{call(ToolProposeMemory, map[string]any{"memory": "朝が得意"}), call(ToolGetProgress, nil)}}, nil
		default:
			if !results[0].Proposal || results[1].Data == nil {
				t.Errorf("proposal acknowledgement: %+v", results)
			}
			return Step{Reply: "ゼミ発表の前に、レポートも入れておこう。", Calls: []ToolCall{
				call(ToolProposeTask, map[string]any{"title": "レポート", "why": "先生に認められたい", "minutes": 30, "deadline": w.deadline(6)}),
			}}, nil
		}
	}
	turn := w.send(t, "u", "tools", "今日なにやればいい？", "")
	if turn.Status != StatusReady || w.model.callCount() != 3 || len(turn.Cards) != 2 || turn.Cards[0].Kind != CardMemory || turn.Cards[1].Input.Why != "先生に認められたい" {
		t.Fatalf("tool loop: %+v", turn)
	}
	if list, _ := w.tasks.List(t.Context(), "u"); len(list.Items) != 1 {
		t.Fatal("a proposal was saved")
	}
}

func TestEndlessToolCallsStopAfterRoundLimit(t *testing.T) {
	w := newWorld(t, reply("", call(ToolGetProgress, nil)))
	turn := w.send(t, "u", "loop", "最近どう？", "")
	if turn.Status != StatusError || turn.Failure != "ai.unreadable" || w.model.callCount() != MaxRounds {
		t.Fatalf("tool rounds were not bounded: %+v (%d calls)", turn, w.model.callCount())
	}
}

func TestInvalidRepliesAndCardsFailTheTurn(t *testing.T) {
	w := newWorld(t, nil)
	memory := func(text string) ToolCall { return call(ToolProposeMemory, map[string]any{"memory": text}) }
	tests := []struct {
		name    string
		script  script
		failure string
	}{
		{"空の返答", reply("  "), "ai.unreadable"},
		{"長すぎる返答", reply(strings.Repeat("あ", MaxReplyRunes+1)), "ai.unreadable"},
		{"カードが多すぎる", reply("ok", memory("a"), memory("b"), memory("c"), memory("d")), "ai.unreadable"},
		{"空の記憶", reply("ok", memory(" ")), "ai.unreadable"},
		{"長い記憶", reply("ok", memory(strings.Repeat("あ", profile.MaxMemoryRunes+1))), "ai.unreadable"},
		{"睡眠の時刻", reply("ok", call(ToolProposeSleep, map[string]any{"bedtime": "25:00", "wake": "07:00"})), "ai.unreadable"},
		{"短すぎる睡眠", reply("ok", call(ToolProposeSleep, map[string]any{"bedtime": "23:00", "wake": "01:00"})), "ai.unreadable"},
		{"締切の形式", reply("ok", call(ToolProposeTask, map[string]any{"title": "x", "minutes": 30, "deadline": "明日"})), "ai.unreadable"},
		{"過去の締切", reply("ok", call(ToolProposeTask, map[string]any{"title": "x", "minutes": 30, "deadline": w.deadline(-1)})), "ai.unreadable"},
		{"長いメモ", reply("ok", call(ToolProposeTask, map[string]any{"title": "x", "minutes": 30, "deadline": w.deadline(4), "notes": strings.Repeat("あ", MaxCardNotesRunes+1)})), "ai.unreadable"},
		{"分数の型", reply("ok", call(ToolProposeTask, map[string]any{"title": "x", "minutes": "30分", "deadline": w.deadline(4)})), "ai.unreadable"},
		{"AI の失敗", func(ConsultInput, int, []ToolResult) (Step, error) {
			return Step{}, apperr.External("ai.failed", errors.New("private"))
		}, "ai.failed"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.model.script = tt.script
			turn, err := w.svc.Send(t.Context(), "u", fmt.Sprintf("invalid-%d", i), "相談", "")
			if err != nil || turn.Status != StatusError || turn.Failure != tt.failure || len(turn.Cards) != 0 {
				t.Fatalf("%+v %v", turn, err)
			}
		})
	}
	w.model.script = reply("ok", call(ToolProposeSleep, map[string]any{"bedtime": "23:30", "wake": "07:00"}))
	if turn := w.send(t, "u", "sleep", "23時半に寝る", ""); turn.Status != StatusReady || turn.Cards[0].Sleep.Bedtime != "23:30" {
		t.Fatalf("sleep card: %+v", turn)
	}
}

func TestRateLimitSavesErrorTurnAndReturnsError(t *testing.T) {
	limited := apperr.RateLimited("ai.rate_limited", 30*time.Second)
	w := newWorld(t, func(ConsultInput, int, []ToolResult) (Step, error) { return Step{}, limited })
	turn, err := w.svc.Send(t.Context(), "u", "limited", "相談", "")
	if !errors.Is(err, limited) || turn.Status != StatusError || turn.Failure != "ai.rate_limited" {
		t.Fatalf("limit: %+v %v", turn, err)
	}
	turns, _ := w.svc.List(t.Context(), "u")
	if len(turns) != 1 || turns[0].Failure != "ai.rate_limited" {
		t.Fatal("the limited turn was not saved")
	}
	w.model.script = reply("ok")
	if turn := w.send(t, "u", "next", "相談", ""); turn.Status != StatusReady {
		t.Fatal("the lease was not released")
	}
}

func TestContextChangedDuringConsultationFailsTurn(t *testing.T) {
	w := newWorld(t, nil)
	w.model.script = func(ConsultInput, int, []ToolResult) (Step, error) {
		w.clock.Advance(time.Second)
		_, rev, _ := w.profiles.Get(context.Background(), "u")
		if _, _, err := w.profiles.Update(context.Background(), "u", profile.Input{Context: "新しい傾向", TimeZone: "Asia/Tokyo", Locale: locale.Ja}, rev); err != nil {
			t.Error(err)
		}
		return Step{Reply: "ok"}, nil
	}
	turn := w.send(t, "u", "changed", "相談", "")
	if turn.Status != StatusError || turn.Failure != "assistant.context_changed" || turn.Reply != "" {
		t.Fatalf("context change: %+v", turn)
	}
}

func TestReadToolsReportProfileProgressAndWork(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return now.Add(time.Duration(h) * time.Hour) }
	done := at(-2)
	s := Snapshot{
		Now: now, Loc: loc, Profile: profile.Profile{Name: "本人", Context: "朝型", TimeZone: "Asia/Tokyo"},
		Tasks: []task.Task{
			{ID: "soon", Title: "近い", Status: task.StatusPending, Plan: task.Plan{Start: at(1)}},
			{ID: "later", Title: "3日後", Status: task.StatusPending, Plan: task.Plan{Start: at(72)}},
			{ID: "done", Title: "済み", Status: task.StatusCompleted, CompletedAt: &done},
		},
		Account: wallet.Account{Mode: wallet.ModeRewards, Balance: 600, DailyLimit: 1800}, WorkingOn: "soon",
		Calendar: CalendarInfo{Sync: "disconnected"},
	}
	today, _ := ReadTool(ToolGetToday, s).(todayResult)
	if len(today.Tasks) != 1 || today.Tasks[0].ID != "soon" || today.WorkingOn != "soon" || today.ViewingMinutesLeft != 10 || today.Now != "2026-11-07T21:00:00+09:00" {
		t.Fatalf("today: %+v", today)
	}
	if p := ReadTool(ToolGetProfile, s).(profileResult); p.Name != "本人" || p.Context != "朝型" {
		t.Fatalf("profile: %+v", p)
	}
	if c := ReadTool(ToolGetCalendar, s).(calendarResult); c.Connected || len(c.Events) != 0 {
		t.Fatalf("calendar: %+v", c)
	}
}

func TestHistoryCardsDropPreviews(t *testing.T) {
	plan := task.Plan{Note: "長い手順"}
	cards := HistoryCards([]Card{{ID: "c1", Kind: CardCreate, InputID: "c1", Input: &task.Input{Title: "レポート", Minutes: 30}, Preview: &plan}})
	if len(cards) != 1 || cards[0].ID != "c1" || cards[0].Input.Title != "レポート" {
		t.Fatalf("history card: %+v", cards)
	}
}

func TestTurnRoundTripsThroughStore(t *testing.T) {
	start := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	turn := Turn{ID: "x", Text: "相談", Status: StatusReady, Reply: "ok", CreatedAt: start, Cards: []Card{
		{ID: "c", Kind: CardUpdate, TaskID: "t", Input: &task.Input{Title: "a", Deadline: start, Minutes: 3}, Revision: 4,
			Draft: &task.Draft{ID: "d", Kind: task.DraftEdit, Changes: []task.Change{{TaskID: "t", After: task.Plan{Start: start, Reason: task.ReasonEditReplanned}}}}},
		{ID: "s", Kind: CardSleep, Sleep: &sleep.Schedule{Bedtime: "23:00", Wake: "07:00"}},
	}}
	doc, err := encodeTurn(turn, "run")
	if err != nil {
		t.Fatal(err)
	}
	back, err := doc.turn()
	if err != nil || back.Cards[0].Draft.Changes[0].After.Reason != task.ReasonEditReplanned || back.Cards[0].Revision != 4 || back.Cards[1].Sleep.Wake != "07:00" || doc.RunID != "run" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}
