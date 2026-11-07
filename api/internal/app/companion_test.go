package app

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompanionReadsStateThroughToolsBeforeProposing(t *testing.T) {
	var calls atomic.Int32
	_, c := assistantClient(t, func(raw string) any {
		if !strings.Contains(raw, "入力JSON") {
			return map[string]any{"steps": []map[string]any{{"title": "資料を開く", "minutes": 30}}}
		}
		if !strings.Contains(raw, "systemInstruction") || !strings.Contains(raw, CompanionName) {
			t.Error("the companion persona was not sent")
		}
		if calls.Add(1) == 1 {
			return map[string]any{"call": toolToday}
		}
		if !strings.Contains(raw, "functionResponse") || !strings.Contains(raw, "ゼミ発表") {
			t.Error("the tool result did not reach Gemini")
		}
		return map[string]any{"reply": "ゼミ発表の前に、レポートも入れておこう。", "cards": []any{
			map[string]any{"kind": "create", "input": map[string]any{"title": "レポート", "why": "先生に認められたい", "minutes": 30, "deadline": time.Now().Add(6 * time.Hour).Format(time.RFC3339)}},
		}}
	})
	existing := input()
	existing.Title = "ゼミ発表"
	c.state("POST", "/api/tasks", existing)
	turn := consultation(t, c, "tools", "今日なにやればいい？")
	if turn.Status != "ready" || calls.Load() != 2 || len(turn.Cards) != 1 {
		t.Fatalf("tool loop: %+v (calls %d)", turn, calls.Load())
	}
	if card := turn.Cards[0]; card.Kind != "create" || card.Input.Why != "先生に認められたい" || card.Preview == nil {
		t.Fatalf("proposal card lost its why or preview: %+v", card)
	}
	if state := c.state("GET", "/api/state", nil); len(state.Tasks) != 1 {
		t.Fatal("a proposal was saved without confirmation")
	}
}

func TestCompanionStopsEndlessToolCalls(t *testing.T) {
	var calls atomic.Int32
	_, c := assistantClient(t, func(string) any {
		calls.Add(1)
		return map[string]any{"call": toolProgress}
	})
	turn := consultation(t, c, "loop", "最近どう？")
	if turn.Status != "error" || calls.Load() != maxToolRounds {
		t.Fatalf("tool rounds were not bounded: %s %d", turn.Status, calls.Load())
	}
}

func line(t *testing.T, c testClient, moment, taskID string) (int, map[string]any) {
	t.Helper()
	code, raw := c.call("POST", "/api/companion/line", map[string]string{"moment": moment, "taskId": taskID})
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return code, out
}

func TestCompanionLineFallsBackWithoutGemini(t *testing.T) {
	c := client(t)
	in := input()
	in.Why = "発表で自信を持ちたい"
	c.state("POST", "/api/tasks", in)
	code, out := line(t, c, "notice", in.ID)
	if code != 200 || out["generated"] != false || !strings.Contains(out["line"].(string), in.Why) {
		t.Fatalf("demo notice line: %d %v", code, out)
	}
	if code, _ := line(t, c, "lecture", ""); code != 400 {
		t.Fatal("unknown moment accepted")
	}
	if code, _ := line(t, c, "notice", "missing"); code != 404 {
		t.Fatal("unknown task accepted")
	}
}

func TestCompanionLineRejectsUnsafeWording(t *testing.T) {
	text := "よし、まず資料を開こう。"
	_, c := assistantClient(t, func(string) any { return text })
	if _, out := line(t, c, "complete", ""); out["generated"] != true || out["line"] != text {
		t.Fatalf("generated line: %v", out)
	}
	text = "別の一言"
	if _, out := line(t, c, "complete", ""); out["line"] != "よし、まず資料を開こう。" {
		t.Fatalf("the same moment was generated again: %v", out)
	}
	text = "それは依存症ですね"
	if _, out := line(t, c, "balance_zero", ""); out["generated"] != false || strings.Contains(out["line"].(string), "依存症") {
		t.Fatalf("banned wording was shown: %v", out)
	}
}

func TestStreakDaysUsesLocalCalendarDays(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, loc)
	state := NewState(now)
	for _, back := range []int{1, 2, 4} {
		at := now.AddDate(0, 0, -back)
		state.Tasks = append(state.Tasks, Task{Status: "completed", CompletedAt: &at})
	}
	if got := streakDays(state, now); got != 2 {
		t.Fatalf("streak through yesterday: %d", got)
	}
	state.Tasks = append(state.Tasks, Task{Status: "completed", CompletedAt: &now})
	if got := streakDays(state, now); got != 3 {
		t.Fatalf("streak including today: %d", got)
	}
}
