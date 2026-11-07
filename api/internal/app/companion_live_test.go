package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/genai"
)

// toolTrace records the function calls Gemini makes so a live run shows how the companion used its tools.
type toolTrace struct {
	next  http.RoundTripper
	calls []string
	text  string
}

func (t *toolTrace) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.next.RoundTrip(r)
	if err != nil {
		return res, err
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body = io.NopCloser(bytes.NewReader(raw))
	var body struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	_ = json.Unmarshal(raw, &body)
	for _, c := range body.Candidates {
		for _, p := range c.Content.Parts {
			t.text += p.Text
			if p.FunctionCall != nil {
				args, _ := json.Marshal(p.FunctionCall.Args)
				t.calls = append(t.calls, p.FunctionCall.Name+string(args))
			}
		}
	}
	return res, nil
}

// TestLiveCompanionScript replays a conversation script against Vertex AI and prints what the companion said.
// The script is a JSON list of {"name", "turns": [...]} or {"name", "line": moment, "task": id}; every turn or line is one question.
func TestLiveCompanionScript(t *testing.T) {
	script := os.Getenv("DOPAGAKI_LIVE_SCRIPT")
	if os.Getenv("DOPAGAKI_LIVE_GCP") != "1" || script == "" {
		t.Skip("explicit live-service test; uses the selected Google Cloud project")
	}
	var cases []struct {
		Name  string   `json:"name"`
		Turns []string `json:"turns"`
		Line  string   `json:"line"`
		Task  string   `json:"task"`
		Sleep bool     `json:"sleep"`
	}
	raw, err := os.ReadFile(script)
	if err != nil || json.Unmarshal(raw, &cases) != nil {
		t.Fatal("unreadable script")
	}
	const project = "devfest-2026-513816"
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	token, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token", "--project="+project).Output()
	if err != nil {
		t.Fatal("gcloud authentication is required")
	}
	trace := &toolTrace{}
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: strings.TrimSpace(string(token))}))
	trace.next, httpClient.Transport = httpClient.Transport, trace
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend: genai.BackendVertexAI, Project: project, Location: "global", HTTPClient: httpClient,
		HTTPOptions: genai.HTTPOptions{Headers: http.Header{"X-Goog-User-Project": []string{project}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	planner := &Planner{Client: client, Model: "gemini-3.8-flash", Usage: NewMemoryAIUsageStore(), Limits: AILimits{60, 300, 60, 300}}
	lineTimeout = 20 * time.Second
	for _, c := range cases {
		state := liveFixture(time.Now())
		if c.Sleep {
			now := time.Now()
			state.Sleep = &Sleep{Bedtime: "23:30", Wake: "07:00", Since: now.AddDate(0, 0, -4), Kept: 2}
			state.Sleep.Settled, _, _ = state.lastNight(now)
		}
		history := []AssistantTurn{}
		for _, text := range c.Turns {
			trace.calls = nil
			began := time.Now()
			reply, cards, err := planner.Consult(ctx, "live-fixture", text, history, state, "")
			t.Logf("\n[%s] 本人: %s\n  道具: %v\n  相棒(%.1fs): %s\n  エラー: %v", c.Name, text, trace.calls, time.Since(began).Seconds(), reply, err)
			for _, card := range cards {
				out, _ := json.Marshal(card)
				t.Logf("  カード: %s", out)
			}
			history = append(history, AssistantTurn{Text: text, Reply: reply, Cards: cards, Status: "ready", CreatedAt: time.Now()})
		}
		if c.Line != "" {
			var task *Task
			if c.Task != "" {
				task, _ = state.task(c.Task)
			}
			trace.text = ""
			began := time.Now()
			line, generated := planner.Line(ctx, "live-fixture", c.Line, state, task)
			t.Logf("\n[%s] 場面 %s → %s（生成:%v %.1fs）", c.Name, c.Line, line, generated, time.Since(began).Seconds())
			if !generated {
				t.Logf("  Geminiの生出力: %q", trace.text)
			}
		}
	}
}

// liveFixture is a fictional student with a seminar tomorrow, a report later this week and a short streak.
func liveFixture(now time.Time) State {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	state := NewState(now)
	state.Profile = Profile{Name: "ユウキ", TimeZone: "Asia/Tokyo", Context: "夜はショートを見がち。作業は午前のほうが集中できる。"}
	for _, back := range []int{1, 2} {
		at := now.AddDate(0, 0, -back)
		state.Tasks = append(state.Tasks, Task{ID: "done" + string(rune('0'+back)), Title: "統計の小テスト対策", Status: "completed", CompletedAt: &at, Minutes: 30})
	}
	day := now.In(loc)
	seminar := time.Date(day.Year(), day.Month(), day.Day()+1, 13, 0, 0, 0, loc)
	report := time.Date(day.Year(), day.Month(), day.Day()+3, 23, 59, 0, 0, loc)
	state.Tasks = append(state.Tasks,
		Task{ID: "seminar", Title: "ゼミ発表のスライド", Why: "先輩みたいに落ち着いて発表したい", Deadline: seminar, Minutes: 90, Status: "pending", CreatedAt: now},
		Task{ID: "report", Title: "社会学レポート2000字", Deadline: report, Minutes: 120, Status: "pending", CreatedAt: now},
	)
	state.replan()
	seminarTask, _ := state.task("seminar")
	seminarTask.Plan.Steps = []Step{{Title: "去年のスライドを開く", Start: seminarTask.Plan.Start, End: seminarTask.Plan.Start.Add(5 * time.Minute)}, {Title: "構成を3枚に書き出す", Start: seminarTask.Plan.Start.Add(5 * time.Minute), End: seminarTask.Plan.End}}
	state.BalanceSeconds = 1500
	return state
}
