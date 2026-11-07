package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"
)

func assistantClient(t *testing.T, reply func(string) any) (*Server, testClient) {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		writeJSON(w, 200, map[string]any{"candidates": []map[string]any{{"content": map[string]any{"role": "model", "parts": fixtureParts(reply(string(raw)))}, "finishReason": "STOP"}}})
	}))
	t.Cleanup(provider.Close)
	model, err := genai.NewClient(t.Context(), &genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: "test-only", HTTPOptions: genai.HTTPOptions{BaseURL: provider.URL}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Mode: "demo", Store: NewMemoryStore(), Assistant: NewMemoryAssistantStore(), Planner: &Planner{Client: model, Model: "fixture", Usage: NewMemoryAIUsageStore(), Limits: DefaultAILimits()}}
	c := testClient{handler: s.Handler(), cookie: &http.Cookie{Name: "dopagaki_demo", Value: "abcdef0123456789abcdef0123456789"}, t: t}
	return s, c
}

var fixtureTools = map[string]string{"create": toolTask, "task": toolReschedule, "update": toolUpdate, "memory": toolMemory, "sleep": toolSleep}

// fixtureParts turns a {"reply","cards"} fixture into a text part plus proposal function calls.
// Other fixtures are returned as JSON text, and {"call": name} as a single read-tool call.
func fixtureParts(value any) []map[string]any {
	if fixture, ok := value.(map[string]any); ok {
		if name, ok := fixture["call"].(string); ok {
			return []map[string]any{{"functionCall": map[string]any{"name": name, "args": map[string]any{}}}}
		}
		if reply, ok := fixture["reply"].(string); ok {
			parts := []map[string]any{{"text": reply}}
			cards, _ := fixture["cards"].([]any)
			for _, card := range cards {
				raw, _ := json.Marshal(card)
				var c struct {
					Kind       string         `json:"kind"`
					Input      map[string]any `json:"input"`
					TaskID     string         `json:"taskId"`
					Memory     string         `json:"memory"`
					Bedtime    string         `json:"bedtime"`
					Wake       string         `json:"wake"`
					ProposalID string         `json:"proposalId"`
				}
				_ = json.Unmarshal(raw, &c)
				args := map[string]any{"taskId": c.TaskID, "memory": c.Memory, "bedtime": c.Bedtime, "wake": c.Wake}
				if c.Input != nil {
					args = c.Input
					if c.ProposalID != "" {
						args["proposalId"] = c.ProposalID
					}
					if c.TaskID != "" {
						args["taskId"] = c.TaskID
					}
				}
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": fixtureTools[c.Kind], "args": args}})
			}
			return parts
		}
	}
	text, _ := json.Marshal(value)
	return []map[string]any{{"text": string(text)}}
}
func consultation(t *testing.T, c testClient, id, text string) AssistantTurn {
	t.Helper()
	code, raw := c.call("POST", "/api/assistant/messages", map[string]string{"id": id, "text": text})
	if code != 200 {
		t.Fatalf("consultation: %d %s", code, raw)
	}
	var turn AssistantTurn
	if err := json.Unmarshal(raw, &turn); err != nil {
		t.Fatal(err)
	}
	return turn
}
func TestAssistantRequiresConfirmationAndIsolatesAccounts(t *testing.T) {
	var calls atomic.Int32
	_, c := assistantClient(t, func(raw string) any {
		if !strings.Contains(raw, "入力JSON") {
			return map[string]any{"steps": []map[string]any{{"title": "構成をまとめて本文を書く", "minutes": 30}}}
		}
		calls.Add(1)
		return map[string]any{"reply": "登録前の候補を用意しました。", "cards": []any{
			map[string]any{"kind": "create", "input": TaskInput{ID: "model-id", Title: "レポート", Minutes: 30, Deadline: time.Now().Add(4 * time.Hour)}},
			map[string]any{"kind": "memory", "memory": "短い作業に区切ると進めやすい"},
		}}
	})
	turn := consultation(t, c, "first", "4時間後までにレポート、必要時間30分。短く区切りたい")
	if turn.Status != "ready" || len(turn.Cards) != 2 || turn.Cards[0].Input.ID == "model-id" {
		t.Fatalf("invalid cards: %+v", turn)
	}
	before := c.state("GET", "/api/state", nil)
	if len(before.Tasks) != 0 || before.BalanceSeconds != 3600 || before.Profile.Context != "" {
		t.Fatal("a proposal changed tasks, rewards, or memory")
	}
	repeated := consultation(t, c, "first", turn.Text)
	if repeated.Cards[0].ID != turn.Cards[0].ID || calls.Load() != 1 {
		t.Fatal("a completed request was generated twice")
	}
	if code, _ := c.call("POST", "/api/assistant/messages", map[string]string{"id": "first", "text": "別の内容"}); code != 409 {
		t.Fatalf("request ID reuse: %d", code)
	}
	memory := turn.Cards[1]
	state := c.state("POST", "/api/assistant/memory", map[string]string{"id": memory.ID, "text": memory.Memory})
	state = c.state("POST", "/api/assistant/memory", map[string]string{"id": memory.ID, "text": memory.Memory})
	if state.Profile.Context != memory.Memory || !state.MemoryReceipts[memory.ID] || state.BalanceSeconds != 3600 {
		t.Fatal("memory was duplicated or granted a reward")
	}
	input := *turn.Cards[0].Input
	revision := turn.Cards[0].Revision
	input.BaseRevision = &revision
	state = c.state("POST", "/api/tasks", input)
	state = c.state("POST", "/api/tasks", input)
	if len(state.Tasks) != 1 || state.BalanceSeconds != 4200 {
		t.Fatal("registering a card twice duplicated the task or credit")
	}
	b := c
	b.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	code, raw := b.call("GET", "/api/assistant", nil)
	var history struct {
		Turns []AssistantTurn `json:"turns"`
	}
	_ = json.Unmarshal(raw, &history)
	if code != 200 || len(history.Turns) != 0 {
		t.Fatal("another user's conversation was returned")
	}
	other := b.state("GET", "/api/state", nil)
	if other.Profile.Context != "" || len(other.MemoryReceipts) != 0 || len(other.Tasks) != 0 {
		t.Fatal("another user's memory or calendar cards were returned")
	}
	if code, _ := b.call("GET", "/api/tasks/"+input.ID+"/draft", nil); code != 404 {
		t.Fatal("another user's task could be edited")
	}
	stale := input
	stale.ID = "stale-card"
	if code, _ := c.call("POST", "/api/tasks", stale); code != 409 {
		t.Fatalf("stale calendar preview: %d", code)
	}
	start := time.Now().Add(90 * time.Minute).Format(time.RFC3339)
	path := fmt.Sprintf("/api/tasks/%s/draft?adjust=true&start=%s&revision=%d", input.ID, url.QueryEscape(start), state.PlanRevision)
	if code, _ := c.call("GET", path, nil); code != 200 {
		t.Fatalf("requested schedule preview: %d", code)
	}
	if code, _ := c.call("POST", "/api/chat", nil); code != 404 {
		t.Fatalf("retired Google Chat endpoint: %d", code)
	}
}

func TestForgottenContextIsNotRecoveredFromOldMessages(t *testing.T) {
	var calls atomic.Int32
	_, c := assistantClient(t, func(raw string) any {
		if calls.Add(1) == 2 && strings.Contains(raw, "忘れる目印") {
			t.Error("removed personal context was resent through chat history")
		}
		return map[string]any{"reply": "作業の傾向を確認しました。", "cards": []any{}}
	})
	consultation(t, c, "before", "忘れる目印という作業の好みがあります")
	c.state("POST", "/api/assistant/memory", map[string]string{"id": "preference", "text": "忘れる目印"})
	c.state("PUT", "/api/profile", Profile{Name: "本人", Context: "", TimeZone: "Asia/Tokyo"})
	turn := consultation(t, c, "after", "今の作業の傾向を教えて")
	if turn.Status != "ready" {
		t.Fatalf("reply after forgetting: %+v", turn)
	}
	code, raw := c.call("GET", "/api/assistant", nil)
	if code != 200 || !strings.Contains(string(raw), "忘れる目印") {
		t.Fatal("editing memory unexpectedly deleted the visible conversation")
	}
}

func TestAssistantRejectsForeignTaskAndCanRetryFailedTurn(t *testing.T) {
	var calls atomic.Int32
	_, c := assistantClient(t, func(string) any {
		if calls.Add(1) == 1 {
			return map[string]any{"reply": "予定を確認します。", "cards": []any{map[string]any{"kind": "task", "taskId": "someone-else"}}}
		}
		return map[string]any{"reply": "課題名と締切を教えてください。", "cards": []any{}}
	})
	turn := consultation(t, c, "retry", "この予定を調整したい")
	if turn.Status != "error" || len(turn.Cards) != 0 {
		t.Fatal("an unknown task was rendered as an actionable card")
	}
	turn = consultation(t, c, "retry", turn.Text)
	if turn.Status != "ready" || calls.Load() != 2 {
		t.Fatal("the failed turn could not be retried")
	}
}

func TestAssistantLeaseAndHistoryRetention(t *testing.T) {
	store := NewMemoryAssistantStore()
	var winners atomic.Int32
	var winning AssistantTurn
	var run string
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			turn, id, err := store.Begin(t.Context(), "one", "request", "相談", "")
			if err == nil {
				winners.Add(1)
				winning, run = turn, id
			} else if err != errAssistantBusy {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("concurrent requests reached Gemini more than once")
	}
	if err := store.Finish(t.Context(), "one", winning, "wrong-run"); err != ErrConflict {
		t.Fatal("another worker released an active request")
	}
	winning.Status, winning.PendingUntil = "ready", nil
	if err := store.Finish(t.Context(), "one", winning, run); err != nil {
		t.Fatal(err)
	}
	for i := range 24 {
		turn, run, err := store.Begin(t.Context(), "one", fmt.Sprintf("turn-%d", i), "相談", "")
		if err != nil {
			t.Fatal(err)
		}
		turn.Status, turn.PendingUntil = "ready", nil
		if err = store.Finish(t.Context(), "one", turn, run); err != nil {
			t.Fatal(err)
		}
	}
	history, _ := store.History(t.Context(), "one")
	if len(history) != 20 || len(store.turns["one"]) != 25 {
		t.Fatal("display limit deleted older turns")
	}
}

func TestAssistantRevisesUnregisteredProposalWithoutDuplicatingTask(t *testing.T) {
	proposalID := ""
	_, c := assistantClient(t, func(raw string) any {
		if proposalID == "" {
			return map[string]any{"reply": "30分の候補です", "cards": []any{
				map[string]any{"kind": "create", "input": TaskInput{Title: "確認用", Minutes: 30, Deadline: time.Now().Add(4 * time.Hour)}},
			}}
		}
		if !strings.Contains(raw, proposalID) {
			t.Fatal("the revision did not receive the previous proposal")
		}
		return map[string]any{"reply": "40分の候補です", "cards": []any{
			map[string]any{"kind": "create", "proposalId": proposalID, "input": TaskInput{Title: "確認用", Minutes: 40, Deadline: time.Now().Add(4 * time.Hour)}},
		}}
	})
	first := consultation(t, c, "original", "確認用を30分で")
	proposalID = first.Cards[0].ID
	second := consultation(t, c, "revised", "さっきの候補を40分に")
	if second.Status != "ready" || len(second.Cards) != 1 {
		t.Fatalf("revision failed: %+v", second)
	}
	card := second.Cards[0]
	if card.ID == proposalID || card.Input.ID != first.Cards[0].Input.ID || card.Input.Minutes != 40 {
		t.Fatal("the revision lost the stable task ID or the updated duration")
	}
	state := c.state("GET", "/api/state", nil)
	if len(state.Tasks) != 0 || state.BalanceSeconds != 3600 {
		t.Fatal("revising a proposal registered a task")
	}
}

func TestAssistantUsesOwnedSelectedTaskAndPreservesContextOnReplay(t *testing.T) {
	var calls atomic.Int32
	_, c := assistantClient(t, func(raw string) any {
		if !strings.Contains(raw, "入力JSON") {
			return map[string]any{"steps": []map[string]any{{"title": "本文を書く", "minutes": 30}}}
		}
		calls.Add(1)
		var request struct {
			Contents []struct{ Parts []struct{ Text string } }
		}
		if err := json.Unmarshal([]byte(raw), &request); err != nil {
			t.Fatal(err)
		}
		prompt := request.Contents[0].Parts[0].Text
		var data struct {
			SelectedTask struct {
				ID    string
				Title string
			}
		}
		if err := json.Unmarshal([]byte(strings.SplitN(prompt, "入力JSON:\n", 2)[1]), &data); err != nil {
			t.Fatal(err)
		}
		if data.SelectedTask.ID != "selected-b" || data.SelectedTask.Title != "同じ名前の課題" {
			t.Fatalf("wrong selection: %+v", data.SelectedTask)
		}
		return map[string]any{"reply": "選んだ予定です", "cards": []any{map[string]any{"kind": "task", "taskId": data.SelectedTask.ID}}}
	})
	for _, id := range []string{"selected-a", "selected-b"} {
		c.state("POST", "/api/tasks", TaskInput{ID: id, Title: "同じ名前の課題", Minutes: 30, Deadline: time.Now().Add(4 * time.Hour)})
	}
	input := map[string]string{"id": "selection-message", "text": "この予定を見直したい", "taskId": "selected-b"}
	code, raw := c.call("POST", "/api/assistant/messages", input)
	var turn AssistantTurn
	_ = json.Unmarshal(raw, &turn)
	if code != 200 || turn.Status != "ready" || turn.TaskID != "selected-b" || turn.Cards[0].TaskID != "selected-b" {
		t.Fatalf("selection lost: %d %s", code, raw)
	}
	c.call("POST", "/api/assistant/messages", input)
	if calls.Load() != 1 {
		t.Fatal("replay regenerated the selected task response")
	}
	input["taskId"] = "selected-a"
	if code, _ = c.call("POST", "/api/assistant/messages", input); code != 409 {
		t.Fatal("same request ID accepted a different selected task")
	}
	other := c
	other.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	input["id"] = "foreign-selection"
	_, raw = other.call("POST", "/api/assistant/messages", input)
	_ = json.Unmarshal(raw, &turn)
	if turn.Status != "error" || calls.Load() != 1 {
		t.Fatal("foreign task was sent to Gemini")
	}
}

func TestAssistantProposesOwnedContentEditWithoutSaving(t *testing.T) {
	in := input()
	in.Title, in.Notes, in.Why, in.Minutes = "レポートの見直し", "考察を追加する", "提出に備える", 45
	_, c := assistantClient(t, func(raw string) any {
		if !strings.Contains(raw, "入力JSON") {
			return map[string]any{"steps": []map[string]any{{"title": "レポートを書く", "minutes": 30}}}
		}
		return map[string]any{"reply": "内容の変更案です。確認してください。", "cards": []any{map[string]any{"kind": "update", "taskId": "one", "input": in}}}
	})
	before := c.state("POST", "/api/tasks", input())
	turn := consultation(t, c, "edit-content", "レポートを45分にして考察を追加したい")
	if turn.Status != "ready" || len(turn.Cards) != 1 || turn.Cards[0].Change == nil || turn.Cards[0].Change.Input.Minutes != 45 || turn.Cards[0].Change.Input.ID != "one" {
		t.Fatalf("invalid edit proposal: %+v", turn)
	}
	after := c.state("GET", "/api/state", nil)
	if after.PlanRevision != before.PlanRevision || after.Tasks[0].Minutes != 30 || after.BalanceSeconds != before.BalanceSeconds {
		t.Fatal("AI saved a proposal without confirmation")
	}
	other := c
	other.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	foreign := consultation(t, other, "foreign-content", "この課題を編集して")
	if foreign.Status != "error" || len(foreign.Cards) != 0 {
		t.Fatal("model invented a foreign task edit")
	}
}
