package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestArchivedCompletionKeepsHistoryAndDoesNotRewardAgain(t *testing.T) {
	c := client(t)
	c.state("POST", "/api/tasks", input())
	c.state("POST", "/api/tasks/one/start", nil)
	done := c.state("POST", "/api/tasks/one/complete", nil)
	for range 2 {
		if code, raw := c.call("POST", "/api/tasks/one/archive", nil); code != 200 {
			t.Fatalf("archive: %d %s", code, raw)
		}
	}
	st := c.state("GET", "/api/state", nil)
	if len(st.Tasks) != 0 || st.BalanceSeconds != done.BalanceSeconds {
		t.Fatal("archive changed wallet or kept task")
	}
	code, raw := c.call("GET", "/api/records", nil)
	if code != 200 {
		t.Fatalf("records: %d %s", code, raw)
	}
	var history struct {
		Completions []struct {
			TaskID string `json:"taskId"`
		} `json:"completions"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Completions) != 1 || history.Completions[0].TaskID != "one" {
		t.Fatal("completion was lost or duplicated")
	}
}

func TestPauseSetsStateInsteadOfTogglingOnRetry(t *testing.T) {
	c := client(t)
	c.state("POST", "/api/tasks", input())
	c.state("POST", "/api/tasks/one/start", nil)
	for range 2 {
		st := c.state("PUT", "/api/session/pause", map[string]any{"paused": true})
		if st.Session.PausedAt == nil {
			t.Fatal("retry resumed paused session")
		}
	}
}

func TestExportHasOwnedData(t *testing.T) {
	c := client(t)
	c.state("POST", "/api/tasks", input())
	code, raw := c.call("GET", "/api/account/export", nil)
	if code != 200 {
		t.Fatalf("export: %d %s", code, raw)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data["state"] == nil {
		t.Fatal("missing state")
	}
	other := c
	other.cookie = &http.Cookie{Name: c.cookie.Name, Value: "0123456789abcdef0123456789abcdef"}
	_, raw = other.call("GET", "/api/account/export", nil)
	var exported struct {
		State State `json:"state"`
	}
	if err := json.Unmarshal(raw, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.State.Tasks) != 0 {
		t.Fatal("foreign task exported")
	}
}

func TestAccountDeletionBlocksOldSessionAndLeavesOtherUser(t *testing.T) {
	c := client(t)
	c.state("POST", "/api/tasks", input())
	other := c
	other.cookie = &http.Cookie{Name: c.cookie.Name, Value: "0123456789abcdef0123456789abcdef"}
	other.state("POST", "/api/tasks", input())
	code, raw := c.call("POST", "/api/account/deletion", map[string]any{"deleteCalendar": false})
	if code != 200 {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if code, _ = c.call("GET", "/api/state", nil); code != 403 {
		t.Fatalf("old identity resurrected account: %d", code)
	}
	if code, _ = c.call("POST", "/api/tasks", input()); code != 403 {
		t.Fatalf("old identity wrote after deletion: %d", code)
	}
	if len(other.state("GET", "/api/state", nil).Tasks) != 1 {
		t.Fatal("deleted another user")
	}
}

func TestCSVCellsCannotBecomeFormulae(t *testing.T) {
	for _, text := range []string{"=1+2", " +cmd", "@SUM(1)", "\t=1", "\r=1"} {
		if csvCell(text) == text {
			t.Errorf("unsafe CSV %q", text)
		}
	}
	for _, text := range []string{"", "普通の課題", "Report 2"} {
		if csvCell(text) != text {
			t.Errorf("changed plain text %q", text)
		}
	}
}

func TestPauseRequiresExplicitState(t *testing.T) {
	c := client(t)
	c.state("POST", "/api/tasks", input())
	c.state("POST", "/api/tasks/one/start", nil)
	if code, _ := c.call("PUT", "/api/session/pause", map[string]any{}); code != 400 {
		t.Fatalf("missing state: %d", code)
	}
	if c.state("GET", "/api/state", nil).Session.PausedAt != nil {
		t.Fatal("invalid pause changed session")
	}
}

func TestSnoozeRejectsUntrustedEventIDs(t *testing.T) {
	c := client(t)
	st := c.state("POST", "/api/tasks", input())
	draft := st.NextDraft
	draft.Intent = "snooze"
	draft.ID = "invalid/path"
	if code, _ := c.call("POST", "/api/draft/commit", draft); code != 400 {
		t.Fatalf("unsafe event ID accepted: %d", code)
	}
}

func TestRecordPagingDoesNotReduceActivityTotals(t *testing.T) {
	store := NewMemoryStore()
	c := client(t)
	c.handler = (&Server{Mode: "demo", Store: store, Planner: &Planner{}, AppURL: "http://127.0.0.1:4200"}).Handler()
	owner := "demo-" + c.cookie.Value
	now := time.Now()
	if _, err := store.Update(t.Context(), owner, func(st *State) error {
		for i := range 105 {
			st.Tasks = append(st.Tasks, Task{ID: fmt.Sprintf("task-%03d", i), Title: "架空の完了記録", Status: "completed", CompletedAt: &now})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var first struct {
		Completions []struct {
			ID string `json:"taskId"`
		} `json:"completions"`
		Next     string `json:"nextCursor"`
		Activity struct {
			Completed int `json:"completed"`
		} `json:"activity"`
	}
	code, raw := c.call("GET", "/api/records", nil)
	if code != 200 {
		t.Fatalf("records: %d %s", code, raw)
	}
	if err := json.Unmarshal(raw, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Completions) != 100 || first.Activity.Completed != 105 || first.Next == "" {
		t.Fatalf("first page: %#v", first)
	}
	var second struct {
		Completions []struct {
			ID string `json:"taskId"`
		} `json:"completions"`
		Next string `json:"nextCursor"`
	}
	_, raw = c.call("GET", "/api/records?cursor="+first.Next, nil)
	if err := json.Unmarshal(raw, &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Completions) != 5 || second.Next != "" {
		t.Fatalf("second page: %#v", second)
	}
	ids := map[string]bool{}
	for _, entry := range append(first.Completions, second.Completions...) {
		if ids[entry.ID] {
			t.Fatal("duplicate page entry")
		}
		ids[entry.ID] = true
	}
}

func TestOnlyConfiguredExtensionOriginIsAccepted(t *testing.T) {
	server := &Server{Mode: "live", AppURL: "https://app.example", ExtensionIDs: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if !server.allowedOrigin("chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") || server.allowedOrigin("chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") || server.allowedOrigin("chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/path") {
		t.Fatal("extension origin allowlist was not enforced")
	}
}

func TestDeletedAccountCannotRestoreAnAssistantTurn(t *testing.T) {
	assistant := NewMemoryAssistantStore()
	store := NewMemoryStore()
	server := &Server{Mode: "demo", Store: store, Assistant: assistant}
	turn, run, err := assistant.Begin(t.Context(), "owner", "turn", "架空の相談", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = server.finishAccountDeletion(t.Context(), "owner", false); err != nil {
		t.Fatal(err)
	}
	if err = assistant.Finish(t.Context(), "owner", turn, run); err == nil {
		t.Fatal("late assistant response restored deleted history")
	}
	if _, _, err = assistant.Begin(t.Context(), "owner", "new-turn", "遅れた相談", ""); err == nil {
		t.Fatal("old account started new assistant history")
	}
}
