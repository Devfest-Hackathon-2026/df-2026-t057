package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/aiquota"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/assistant"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// fakeVertex は Vertex AI の generateContent を真似る。reply は受け取った本文から、応答の parts（か HTTP エラー）を返す。
type fakeVertex struct {
	mu     sync.Mutex
	bodies []string
	paths  []string
	reply  func(body string) (status int, parts []map[string]any)
}

func (f *fakeVertex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.bodies = append(f.bodies, string(raw))
	f.paths = append(f.paths, r.URL.Path)
	reply := f.reply
	f.mu.Unlock()
	status, parts := reply(string(raw))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusOK {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "status": "INTERNAL", "message": "private-upstream-detail"}})
		return
	}
	body := map[string]any{"candidates": []map[string]any{}}
	if parts != nil {
		body["candidates"] = []map[string]any{{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeVertex) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *fakeVertex) body(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[i]
}

func textParts(v any) []map[string]any {
	if s, ok := v.(string); ok {
		return []map[string]any{{"text": s}}
	}
	raw, _ := json.Marshal(v)
	return []map[string]any{{"text": string(raw)}}
}

type countingQuota struct {
	calls atomic.Int32
	err   error
}

func (q *countingQuota) Take(context.Context, string) error {
	q.calls.Add(1)
	return q.err
}

func newClient(t *testing.T, quota Quota, reply func(string) (int, []map[string]any)) (*Client, *fakeVertex) {
	t.Helper()
	fake := &fakeVertex{reply: reply}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	c, err := New(t.Context(), Config{Project: "fixture-project", Location: "us-central1", Model: "fixture-model", BaseURL: server.URL, HTTPClient: server.Client()}, quota)
	if err != nil {
		t.Fatal(err)
	}
	return c, fake
}

func ok(v any) func(string) (int, []map[string]any) {
	return func(string) (int, []map[string]any) { return http.StatusOK, textParts(v) }
}

func TestDetailSendsPersonaAndProfileToVertex(t *testing.T) {
	quota := &countingQuota{}
	c, fake := newClient(t, quota, ok(map[string]any{
		"steps": []map[string]any{{"title": " 構成を箇条書きにする ", "minutes": 10}, {"title": "本文を書く", "minutes": 20}},
		"note":  strings.Repeat("長", task.MaxNoteRunes+1),
	}))
	res, err := c.Detail(t.Context(), "u", task.DetailRequest{Title: "レポート", Minutes: 30, Context: "文章の書き出しが苦手"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 2 || res.Steps[0].Title != "構成を箇条書きにする" || res.Steps[1].Minutes != 20 || res.Note != "" {
		t.Fatalf("result: %+v", res)
	}
	body := fake.body(0)
	if !strings.Contains(body, "文章の書き出しが苦手") || !strings.Contains(body, "systemInstruction") || !strings.Contains(body, CompanionName) || !strings.Contains(body, "合計は必ず30分") {
		t.Fatal("profile, persona or minutes were not sent")
	}
	if want := "/projects/fixture-project/locations/us-central1/publishers/google/models/fixture-model:generateContent"; !strings.HasSuffix(fake.paths[0], want) {
		t.Fatalf("path: %s", fake.paths[0])
	}
	if quota.calls.Load() != 1 {
		t.Fatal("quota was not taken")
	}
}

func TestUnreadableOutput(t *testing.T) {
	c, _ := newClient(t, &countingQuota{}, ok("これはJSONではありません"))
	if _, err := c.Detail(t.Context(), "u", task.DetailRequest{Minutes: 30}); apperr.As(err).Code != "ai.unreadable" {
		t.Fatalf("detail: %v", err)
	}
	if _, err := c.Extract(t.Context(), "u", "課題", time.Now(), time.UTC); apperr.As(err).Code != "ai.unreadable" {
		t.Fatalf("extract: %v", err)
	}
	if _, err := c.Adjust(t.Context(), "u", task.AdjustRequest{}); apperr.As(err).Code != "ai.unreadable" {
		t.Fatalf("adjust: %v", err)
	}
	empty, _ := newClient(t, &countingQuota{}, func(string) (int, []map[string]any) { return http.StatusOK, nil })
	if _, err := empty.Consult(assistant.ConsultInput{}).Next(t.Context(), "u", nil); apperr.As(err).Code != "ai.unreadable" {
		t.Fatalf("consult without candidates: %v", err)
	}
}

func TestUpstreamFailureIsSingleAttemptAndHidesDetails(t *testing.T) {
	c, fake := newClient(t, &countingQuota{}, func(string) (int, []map[string]any) { return http.StatusServiceUnavailable, nil })
	_, err := c.Extract(t.Context(), "u", "課題", time.Now(), time.UTC)
	e := apperr.As(err)
	if e.Code != "ai.failed" || e.Kind != apperr.KindUnavailable || strings.Contains(err.Error(), "private-upstream-detail") {
		t.Fatalf("unsafe error: %v", err)
	}
	if fake.calls() != 1 {
		t.Fatalf("SDK retried: %d calls", fake.calls())
	}
}

func TestQuotaRejectionMakesNoRequest(t *testing.T) {
	limited := apperr.RateLimited("ai.rate_limited", time.Minute)
	c, fake := newClient(t, &countingQuota{err: limited}, ok("x"))
	if _, err := c.Line(t.Context(), "u", companion.LineRequest{Moment: companion.MomentComplete}); !errors.Is(err, limited) {
		t.Fatalf("line: %v", err)
	}
	if fake.calls() != 0 {
		t.Fatal("a request was made without quota")
	}
}

func TestAllOperationsShareQuotaAndFailedAttemptsCount(t *testing.T) {
	c0 := clock.NewFake(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	quota := aiquota.New(store.NewRunner(memory.NewStore()), c0, aiquota.Limits{UserPerMinute: 4, UserPerDay: 4, GlobalPerMinute: 10, GlobalPerDay: 10})
	c, fake := newClient(t, quota, func(string) (int, []map[string]any) { return http.StatusServiceUnavailable, nil })
	_, _ = c.Extract(t.Context(), "a", "課題", c0.Now(), time.UTC)
	_, _ = c.Detail(t.Context(), "a", task.DetailRequest{Minutes: 30})
	_, _ = c.Adjust(t.Context(), "a", task.AdjustRequest{})
	_, _ = c.Consult(assistant.ConsultInput{}).Next(t.Context(), "a", nil)
	_, err := c.Line(t.Context(), "a", companion.LineRequest{Moment: companion.MomentNotice})
	if e := apperr.As(err); fake.calls() != 4 || e.Code != "ai.rate_limited" || e.RetryAfter <= 0 {
		t.Fatalf("quota bypass: calls=%d err=%v", fake.calls(), err)
	}
	_, _ = c.Extract(t.Context(), "b", "課題", c0.Now(), time.UTC)
	if fake.calls() != 5 {
		t.Fatal("one user's limit blocked another user")
	}
}

func TestExtract(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, loc)
	c, fake := newClient(t, &countingQuota{}, ok(map[string]any{"tasks": []map[string]any{
		{"title": "レポート", "notes": "", "why": "単位", "deadline": "2026-11-08T17:00:00+09:00", "minutes": 60, "url": ""},
	}}))
	inputs, err := c.Extract(t.Context(), "u", "明日17時までにレポート1時間", now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].Title != "レポート" || inputs[0].Minutes != 60 || !inputs[0].Deadline.Equal(time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("inputs: %+v", inputs)
	}
	if body := fake.body(0); !strings.Contains(body, "2026-11-07T09:00:00+09:00") || !strings.Contains(body, "Asia/Tokyo") {
		t.Fatal("now/time zone were not sent in the user's zone")
	}
	none, _ := newClient(t, &countingQuota{}, ok(map[string]any{"tasks": []any{}}))
	if _, err := none.Extract(t.Context(), "u", "?", now, loc); apperr.As(err).Code != "ai.no_tasks" {
		t.Fatalf("no tasks: %v", err)
	}
	bad, _ := newClient(t, &countingQuota{}, ok(map[string]any{"tasks": []map[string]any{{"title": "x", "deadline": "明日", "minutes": 30}}}))
	if _, err := bad.Extract(t.Context(), "u", "?", now, loc); apperr.As(err).Code != "ai.unreadable" {
		t.Fatalf("bad deadline: %v", err)
	}
}

func TestAdjustParsesChanges(t *testing.T) {
	start := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	c, fake := newClient(t, &countingQuota{}, ok(map[string]any{"changes": []map[string]any{{
		"taskId": "t1", "start": "2026-11-07T19:00:00+09:00", "end": "2026-11-07T19:30:00+09:00", "note": "夕方に寄せた",
		"steps": []map[string]any{{"title": "資料を開く", "start": "2026-11-07T19:00:00+09:00", "end": "2026-11-07T19:30:00+09:00"}},
	}}}))
	before := task.Plan{Start: start, End: start.Add(30 * time.Minute), Steps: []task.Step{{Title: "a", Start: start, End: start.Add(30 * time.Minute)}}}
	changes, err := c.Adjust(t.Context(), "u", task.AdjustRequest{
		Changes: []task.Change{{TaskID: "t1", Title: "課題", Before: before, After: before, OriginalMinutes: 30}}, Instruction: "夕方にして", Now: start, Location: time.UTC,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].TaskID != "t1" || !changes[0].After.Start.Equal(start) || changes[0].After.Note != "夕方に寄せた" ||
		len(changes[0].After.Steps) != 1 || changes[0].After.Reason != task.ReasonAIAdjusted {
		t.Fatalf("changes: %+v", changes)
	}
	if body := fake.body(0); !strings.Contains(body, "夕方にして") || !strings.Contains(body, "t1") {
		t.Fatal("instruction or candidates were not sent")
	}
}

func TestLine(t *testing.T) {
	c, fake := newClient(t, &countingQuota{}, ok("よし、始めよう。"))
	line, err := c.Line(t.Context(), "u", companion.LineRequest{Moment: companion.MomentNotice, Task: &companion.TaskSummary{Title: "レポート", Why: "単位を落とさない"}})
	if err != nil || line != "よし、始めよう。" {
		t.Fatalf("line: %q %v", line, err)
	}
	if body := fake.body(0); !strings.Contains(body, "単位を落とさない") || !strings.Contains(body, "予定の開始時刻が来た") || !strings.Contains(body, CompanionName) {
		t.Fatal("moment, task or persona were not sent")
	}
}

func TestConsultLoopsThroughToolsAndKeepsThoughtSignatures(t *testing.T) {
	var round atomic.Int32
	c, fake := newClient(t, &countingQuota{}, func(string) (int, []map[string]any) {
		if round.Add(1) == 1 {
			return http.StatusOK, []map[string]any{{"functionCall": map[string]any{"id": "call-1", "name": assistant.ToolGetToday, "args": map[string]any{}}, "thoughtSignature": "c2lnbmF0dXJl"}}
		}
		return http.StatusOK, []map[string]any{
			{"text": "レポートを入れておこう。"},
			{"functionCall": map[string]any{"name": assistant.ToolProposeTask, "args": map[string]any{"title": "レポート", "minutes": 30}}},
		}
	})
	conv := c.Consult(assistant.ConsultInput{User: "今日なにやればいい？", TimeZone: "Asia/Tokyo"})
	step, err := conv.Next(t.Context(), "u", nil)
	if err != nil || len(step.Calls) != 1 || step.Calls[0].Name != assistant.ToolGetToday || step.Calls[0].ID != "call-1" || step.Reply != "" {
		t.Fatalf("first step: %+v %v", step, err)
	}
	first := fake.body(0)
	if !strings.Contains(first, "今日なにやればいい？") || !strings.Contains(first, "propose_reschedule") || !strings.Contains(first, CompanionName) {
		t.Fatal("prompt, tools or persona were not sent")
	}
	step, err = conv.Next(t.Context(), "u", []assistant.ToolResult{{ID: "call-1", Name: assistant.ToolGetToday, Data: map[string]any{"tasks": []string{"ゼミ発表"}}}})
	if err != nil || step.Reply != "レポートを入れておこう。" || len(step.Calls) != 1 || step.Calls[0].Args["title"] != "レポート" {
		t.Fatalf("second step: %+v %v", step, err)
	}
	second := fake.body(1)
	if !strings.Contains(second, "functionResponse") || !strings.Contains(second, "ゼミ発表") || !strings.Contains(second, "c2lnbmF0dXJl") || !strings.Contains(second, "call-1") {
		t.Fatal("tool result or thought signature did not reach Gemini")
	}
	_, _ = conv.Next(t.Context(), "u", []assistant.ToolResult{{Name: assistant.ToolProposeMemory, Proposal: true}})
	if !strings.Contains(fake.body(2), "本人の確認待ち") {
		t.Fatal("proposal acknowledgement was not sent")
	}
}
