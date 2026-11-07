package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/genai"
)

func TestLiveGeminiPlanning(t *testing.T) {
	if os.Getenv("DOPAGAKI_LIVE_GCP") != "1" {
		t.Skip("explicit live-service test; uses the selected Google Cloud project")
	}
	const project = "devfest-2026-513816"
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	token, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token", "--project="+project).Output()
	if err != nil {
		t.Fatal("gcloud authentication is required")
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend: genai.BackendVertexAI, Project: project, Location: "global",
		HTTPClient:  oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: strings.TrimSpace(string(token))})),
		HTTPOptions: genai.HTTPOptions{Headers: http.Header{"X-Goog-User-Project": []string{project}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	planner := Planner{Client: client, Model: "gemini-3.8-flash", Usage: NewMemoryAIUsageStore(), Limits: DefaultAILimits()}
	deadline := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	profile := Profile{Name: "接続確認", TimeZone: "Asia/Tokyo", Context: "最初に構成を5分考え、その後に本文を書きたい。架空の課題による接続テスト。"}
	inputs, err := planner.Parse(ctx, "fixture-user", fmt.Sprintf("架空のレポートを%sまでに仕上げる。必要時間は30分。作業URLはなし。", deadline.Format(time.RFC3339)), profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].Minutes != 30 || !inputs[0].Deadline.Equal(deadline) {
		t.Fatal("task extraction did not preserve the explicit time and deadline")
	}
	task := Task{Title: inputs[0].Title, Notes: inputs[0].Notes, Minutes: inputs[0].Minutes, Deadline: inputs[0].Deadline}
	plan, err := planner.Detail(ctx, "fixture-user", task, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) == 0 || plan.End.Sub(plan.Start) != 30*time.Minute {
		t.Fatal("concrete plan did not preserve the work duration")
	}
	t.Logf("Vertex AI: extracted %d task, generated %d steps, total 30 minutes", len(inputs), len(plan.Steps))
}
