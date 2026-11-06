package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	firebaseauth "firebase.google.com/go/v4/auth"
)

func TestGoogleIdentityUsesVerifiedProviderSubject(t *testing.T) {
	token := &firebaseauth.Token{UID: "firebase-user", Claims: map[string]interface{}{"email": "user@example.com"}, Firebase: firebaseauth.FirebaseInfo{Identities: map[string]interface{}{"google.com": []interface{}{"google-subject"}}}}
	account := tokenGoogleAccount(token)
	if account.Subject != "google-subject" || account.Email != "user@example.com" {
		t.Fatal("Google identity was confused with the Firebase UID")
	}
	if tokenGoogleAccount(&firebaseauth.Token{}).Subject != "" {
		t.Fatal("missing Google identity was accepted")
	}
}

func TestAccountsCannotReadOrCompleteEachOthersTasks(t *testing.T) {
	a := client(t)
	b := a
	b.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	a.state("POST", "/api/tasks", input())
	if s := b.state("GET", "/api/state", nil); len(s.Tasks) != 0 || s.BalanceSeconds != 3600 {
		t.Fatal("new account inherited another account's tasks or credit")
	}
	for _, path := range []string{"/api/tasks/one/start", "/api/tasks/one/complete"} {
		if code, _ := b.call("POST", path, nil); code != 404 {
			t.Fatalf("cross-account operation %s: %d", path, code)
		}
	}
	if code, _ := b.call("GET", "/api/tasks/one/draft", nil); code != 404 {
		t.Fatalf("cross-account preview: %d", code)
	}
	other := input()
	other.Title = "別の人の課題"
	b.state("POST", "/api/tasks", other)
	b.state("POST", "/api/tasks/one/complete", nil)
	if s := a.state("GET", "/api/state", nil); s.Tasks[0].Status != "pending" || s.BalanceSeconds != 4200 {
		t.Fatal("same task ID changed another account")
	}
}

type testClient struct {
	handler http.Handler
	cookie  *http.Cookie
	t       *testing.T
}

func client(t *testing.T) testClient {
	return testClient{handler: (&Server{Mode: "demo", Store: NewMemoryStore(), Planner: &Planner{}, AppURL: "http://127.0.0.1:4200"}).Handler(), cookie: &http.Cookie{Name: "dopagaki_demo", Value: "abcdef0123456789abcdef0123456789"}, t: t}
}
func (c testClient) call(method, path string, body any) (int, []byte) {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func (c testClient) state(method, path string, body any) State {
	c.t.Helper()
	code, raw := c.call(method, path, body)
	if code != 200 {
		c.t.Fatalf("%s %s: %d %s", method, path, code, raw)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}
func input() TaskInput {
	return TaskInput{ID: "one", Title: "課題", Deadline: time.Now().Add(2 * time.Hour), Minutes: 30}
}

func TestTaskLifecycleRewardsAndTimer(t *testing.T) {
	c := client(t)
	s := c.state("GET", "/api/state", nil)
	if s.BalanceSeconds != 3600 {
		t.Fatal("initial credit")
	}
	s = c.state("POST", "/api/tasks", input())
	if s.BalanceSeconds != 4200 {
		t.Fatal("registration credit")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			code, _ := c.call("POST", "/api/tasks", input())
			if code != 200 {
				t.Errorf("registration retry %d", code)
			}
		})
	}
	wg.Wait()
	s = c.state("GET", "/api/state", nil)
	if len(s.Tasks) != 1 || s.BalanceSeconds != 4200 {
		t.Fatal("retry duplicated a task or reward")
	}
	s = c.state("POST", "/api/tasks/one/start", nil)
	started := s.Session.StartedAt
	s = c.state("POST", "/api/tasks/one/start", nil)
	if !s.Session.StartedAt.Equal(started) {
		t.Fatal("start retry reset timer")
	}
	s = c.state("POST", "/api/session/pause", nil)
	if s.Session.PausedAt == nil {
		t.Fatal("not paused")
	}
	s = c.state("POST", "/api/session/pause", nil)
	if s.Session.PausedAt != nil {
		t.Fatal("not resumed")
	}
	for range 8 {
		wg.Go(func() {
			code, _ := c.call("POST", "/api/tasks/one/complete", nil)
			if code != 200 {
				t.Errorf("complete %d", code)
			}
		})
	}
	wg.Wait()
	s = c.state("GET", "/api/state", nil)
	if s.BalanceSeconds != 5400 || s.Session != nil || s.Tasks[0].Status != "completed" {
		t.Fatal("completion was not exactly once")
	}
}

func TestDraftPreviewCancelAndStaleCommit(t *testing.T) {
	c := client(t)
	s := c.state("POST", "/api/tasks", input())
	draft := s.NextDraft
	if draft == nil {
		t.Fatal("candidate not precomputed")
	}
	current := c.state("GET", "/api/state", nil)
	if !current.Tasks[0].Plan.Start.Equal(s.Tasks[0].Plan.Start) {
		t.Fatal("preview committed")
	}
	settings := s.Settings
	settings.SnoozeMinutes = 15
	c.state("PUT", "/api/settings", settings)
	code, _ := c.call("POST", "/api/draft/commit", draft)
	if code != 409 {
		t.Fatalf("stale commit returned %d", code)
	}
	s = c.state("GET", "/api/state", nil)
	draft = s.NextDraft
	s = c.state("POST", "/api/draft/commit", draft)
	if !s.Tasks[0].Plan.Start.Equal(draft.Changes[0].After.Start) || s.BalanceSeconds != 4200 {
		t.Fatal("commit changed credit or did not apply proposal")
	}
}

func TestViewingReceiptsAndUserIsolation(t *testing.T) {
	c := client(t)
	body := map[string]any{"id": "receipt-001", "seconds": 10}
	s := c.state("POST", "/api/viewing", body)
	if s.BalanceSeconds != 3590 {
		t.Fatal("viewing debit")
	}
	s = c.state("POST", "/api/viewing", body)
	if s.BalanceSeconds != 3590 {
		t.Fatal("retry charged twice")
	}
	s = c.state("POST", "/api/viewing", map[string]any{"id": "receipt-002", "seconds": 10})
	if s.BalanceSeconds < 3589 {
		t.Fatal("parallel tab overcharged")
	}
	other := c
	other.cookie = &http.Cookie{Name: "dopagaki_demo", Value: "0123456789abcdef0123456789abcdef"}
	s = other.state("GET", "/api/state", nil)
	if s.BalanceSeconds != 3600 {
		t.Fatal("user data leaked")
	}
}

func TestLiveRequiresAuthAndDemoRejectsCrossOrigin(t *testing.T) {
	handler := (&Server{Mode: "live", Store: NewMemoryStore()}).Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/state", nil))
	if w.Code != 401 {
		t.Fatal("live route unprotected")
	}
	c := client(t)
	r := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(`{}`))
	r.Header.Set("Origin", "https://untrusted.example")
	w = httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted")
	}
	code, _ := c.call("POST", "/api/tasks", TaskInput{ID: "../bad", Title: "task", Minutes: 10, Deadline: time.Now().Add(time.Hour)})
	if code != 400 {
		t.Fatal("invalid task id accepted")
	}
}

func TestDemoExtensionTokenSharesOnlyItsUser(t *testing.T) {
	c := client(t)
	s := c.state("POST", "/api/tasks", input())
	code, raw := c.call("POST", "/api/extension/token", nil)
	if code != 200 {
		t.Fatal(code)
	}
	var token struct {
		Token string `json:"token"`
	}
	json.Unmarshal(raw, &token)
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Header.Set("Authorization", token.Token)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	var extensionState State
	json.Unmarshal(w.Body.Bytes(), &extensionState)
	if len(extensionState.Tasks) != len(s.Tasks) || extensionState.BalanceSeconds != s.BalanceSeconds {
		t.Fatal("extension connected to a different user")
	}
}
