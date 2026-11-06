package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func editPreview(t *testing.T, c testClient, in TaskInput) TaskEdit {
	t.Helper()
	code, raw := c.call("POST", "/api/tasks/"+in.ID+"/preview", in)
	if code != 200 {
		t.Fatalf("preview: %d %s", code, raw)
	}
	var change TaskEdit
	if err := json.Unmarshal(raw, &change); err != nil {
		t.Fatal(err)
	}
	return change
}

func TestTaskEditPreviewCommitRewardsAndIsolation(t *testing.T) {
	c := client(t)
	initial := input()
	initial.Why = "元の理由"
	before := c.state("POST", "/api/tasks", initial)
	in := initial
	in.Title, in.Notes, in.Why, in.URL = "変更後の課題", "本文と見直し", "新しい理由", "https://example.com/work"
	in.Minutes, in.Deadline = 45, initial.Deadline.Add(time.Hour)
	change := editPreview(t, c, in)
	unchanged := c.state("GET", "/api/state", nil)
	if unchanged.PlanRevision != before.PlanRevision || unchanged.Tasks[0].Title != initial.Title || unchanged.BalanceSeconds != 4200 {
		t.Fatal("preview saved a task or reward")
	}
	other := c
	other.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	if code, _ := other.call("POST", "/api/tasks/one/preview", in); code != 404 {
		t.Fatalf("foreign preview: %d", code)
	}
	if code, _ := other.call("PUT", "/api/tasks/one", change); code != 404 {
		t.Fatalf("foreign commit: %d", code)
	}
	after := c.state("PUT", "/api/tasks/one", change)
	task := after.Tasks[0]
	if len(after.Tasks) != 1 || after.BalanceSeconds != 4200 || len(after.Ledger) != len(before.Ledger) || task.Title != in.Title || task.Notes != in.Notes || task.Why != in.Why || task.URL != in.URL || task.Minutes != in.Minutes || !task.Deadline.Equal(in.Deadline) || !task.CreatedAt.Equal(before.Tasks[0].CreatedAt) {
		t.Fatal("edit duplicated rewards, lost identity, or did not save content")
	}
	if len(task.Plan.Steps) != 1 || task.Plan.Steps[0].Title != in.Title {
		t.Fatal("old steps were retained after changing content")
	}
	if code, _ := c.call("PUT", "/api/tasks/one", change); code != 409 {
		t.Fatalf("stale retry: %d", code)
	}
	c.state("POST", "/api/tasks/one/start", nil)
	if code, _ := c.call("POST", "/api/tasks/one/preview", in); code != 400 {
		t.Fatal("editing active work accepted")
	}
}

func TestTaskEditRejectsStaleAndInvalidTimetable(t *testing.T) {
	c := client(t)
	original := input()
	c.state("POST", "/api/tasks", original)
	in := original
	in.Minutes = 60
	change := editPreview(t, c, in)
	additional := original
	additional.ID, additional.Title = "two", "追加の課題"
	c.state("POST", "/api/tasks", additional)
	if code, _ := c.call("PUT", "/api/tasks/one", change); code != 409 {
		t.Fatal("concurrent plan overwritten")
	}
	impossible := in
	impossible.Deadline, impossible.Minutes = time.Now().Add(2*time.Minute), 720
	if code, _ := c.call("POST", "/api/tasks/one/preview", impossible); code != 400 {
		t.Fatal("past timetable accepted")
	}
	before := c.state("GET", "/api/state", nil)
	change = editPreview(t, c, in)
	change.Draft.Changes[0].TaskID = "someone-else"
	change.Draft.Changes[0].After.Start = time.Now().Add(-time.Hour)
	after := c.state("PUT", "/api/tasks/one", change)
	if after.BalanceSeconds != before.BalanceSeconds || len(after.Tasks) != 2 || after.Tasks[0].Plan.Start.Before(time.Now()) {
		t.Fatal("client supplied timetable was trusted")
	}
}

func TestTaskEditDoesNotOverwriteChangesDuringModelCall(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server, c := assistantClient(t, func(raw string) any {
		if strings.Contains(raw, "新しい内容") {
			close(entered)
			<-release
			return map[string]any{"steps": []map[string]any{{"title": "新しい手順", "minutes": 45}}}
		}
		return map[string]any{"steps": []map[string]any{{"title": "元の手順", "minutes": 30}}}
	})
	c.state("POST", "/api/tasks", input())
	in := input()
	in.Title, in.Minutes = "新しい内容", 45
	change := editPreview(t, c, in)
	done := make(chan int, 1)
	go func() { code, _ := c.call("PUT", "/api/tasks/one", change); done <- code }()
	<-entered
	_, err := server.Store.Update(t.Context(), "demo-"+c.cookie.Value, func(st *State) error { st.Profile.Context = "作業傾向を更新"; return nil })
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != 409 {
		t.Fatalf("profile changed while planning: %d", code)
	}
	after := c.state("GET", "/api/state", nil)
	if after.Tasks[0].Title != "課題" || after.Profile.Context != "作業傾向を更新" || after.BalanceSeconds != 4200 {
		t.Fatal("concurrent change overwritten or reward duplicated")
	}
}
