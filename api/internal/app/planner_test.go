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
			planner := Planner{Client: client, Model: "gemini-3.8-flash"}
			plan, err := planner.Detail(t.Context(), Task{Title: "レポート", Minutes: 30, Deadline: time.Now().Add(time.Hour)}, Profile{Context: "文章の書き出しが苦手"})
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
