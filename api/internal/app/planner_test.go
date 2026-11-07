package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestGeminiDetailUsesProfileAndRejectsWrongDuration(t *testing.T) {
	for _, total := range []int{20, 30} {
		t.Run(time.Duration(total).String(), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(raw), "文章の書き出しが苦手") {
					t.Error("profile was not provided to Gemini")
				}
				text, _ := json.Marshal(map[string]any{"steps": []map[string]any{{"title": "構成を箇条書きにする", "minutes": 10}, {"title": "本文を書く", "minutes": total - 10}}})
				writeJSON(w, 200, map[string]any{"candidates": []map[string]any{{"content": map[string]any{"role": "model", "parts": []map[string]string{{"text": string(text)}}}, "finishReason": "STOP"}}})
			}))
			defer server.Close()
			client, err := genai.NewClient(t.Context(), &genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: "test-only", HTTPOptions: genai.HTTPOptions{BaseURL: server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			planner := Planner{Client: client, Model: "gemini-3.8-flash", Usage: NewMemoryAIUsageStore(), Limits: DefaultAILimits()}
			plan, err := planner.Detail(t.Context(), "fixture-user", Task{Title: "レポート", Minutes: 30, Deadline: time.Now().Add(time.Hour)}, Profile{Context: "文章の書き出しが苦手"})
			if total == 20 {
				if err == nil {
					t.Fatal("incorrect model duration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Steps) != 2 || plan.Steps[0].Title != "構成を箇条書きにする" {
				t.Fatal("concrete steps lost")
			}
		})
	}
}

func TestGeminiFailureDetailsAreNotReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{
			"code": 400, "status": "INVALID_ARGUMENT", "message": "private-upstream-detail",
		}})
	}))
	defer server.Close()
	client, err := genai.NewClient(t.Context(), &genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: "test-only", HTTPOptions: genai.HTTPOptions{BaseURL: server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	planner := &Planner{Client: client, Model: "test", Usage: NewMemoryAIUsageStore(), Limits: DefaultAILimits()}
	for _, operation := range []string{"parse", "adjust"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			if operation == "parse" {
				_, err = planner.Parse(t.Context(), "fixture-user", "課題", Profile{})
			} else {
				_, err = planner.Adjust(t.Context(), "fixture-user", Draft{}, "調整して", Profile{})
			}
			if err == nil || strings.Contains(err.Error(), "private-upstream-detail") || !strings.Contains(err.Error(), "もう一度") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}
