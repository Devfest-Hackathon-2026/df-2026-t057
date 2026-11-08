package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apihttp "github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/realtime"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
)

const demoAuth = "Demo 0123456789abcdef0123456789abcdef"

type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, token string) (apihttp.Principal, error) {
	if token == "good" {
		return apihttp.Principal{UID: "firebase-user"}, nil
	}
	return apihttp.Principal{}, apperr.Unauthenticated("auth.expired")
}

func newServer(t *testing.T, mode config.Mode) (*httptest.Server, *testkit.World) {
	t.Helper()
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	cfg := config.Config{Mode: mode, AppURL: "http://app.test", ExtensionIDs: []string{"abcdefghijklmnopabcdefghijklmnop"}}
	api, err := apihttp.NewAPI(apihttp.Deps{
		Config: cfg, Clock: w.Clock, Log: w.Log, Verifier: fakeVerifier{}, Idempotency: w.Idempotency,
		Profiles: w.Profiles, Tasks: w.Tasks, Work: w.Work, Wallet: w.Wallet, Viewing: w.Viewing, Sleep: w.Sleep,
		Records: w.Records, Shortcut: w.Shortcut, Events: realtime.NewHub(w.Store, w.Log),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(apihttp.APIOnly(api))
	t.Cleanup(srv.Close)
	return srv, w
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func call(t *testing.T, srv *httptest.Server, method, path, body string, headers ...string) resp {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", demoAuth)
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	r := resp{status: res.StatusCode, header: res.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &r.body)
	return r
}

func expectProblem(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || r.body["code"] != code || r.header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("got %d %s (%s), want %d %s", r.status, r.raw, r.header.Get("Content-Type"), status, code)
	}
}

var taskBody = func() string {
	return fmt.Sprintf(`{"title":"レポート","notes":"","why":"","deadline":%q,"minutes":30,"url":""}`, time.Date(2026, 11, 8, 18, 0, 0, 0, testkit.JST).Format(time.RFC3339))
}()

func TestProblemDetailsAndStrictJSON(t *testing.T) {
	srv, _ := newServer(t, config.ModeDemo)
	expectProblem(t, call(t, srv, "GET", "/api/nope", ""), 404, "not_found")
	for _, body := range []string{"null", "{}{}", `{"title":"x"} garbage`, `[]`, ""} {
		expectProblem(t, call(t, srv, "POST", "/api/tasks", body, "Idempotency-Key", "strict-001"), 400, "request.malformed")
	}
	expectProblem(t, call(t, srv, "POST", "/api/tasks", strings.TrimSuffix(taskBody, "}")+`,"extra":1}`, "Idempotency-Key", "strict-002"), 400, "request.malformed")
	expectProblem(t, call(t, srv, "POST", "/api/tasks", taskBody+strings.Repeat(" ", 64<<10), "Idempotency-Key", "strict-003"), 413, "request.too_large")
	r := call(t, srv, "POST", "/api/tasks", `{"title":"x","notes":"","why":"","minutes":30,"url":""}`, "Idempotency-Key", "strict-004")
	expectProblem(t, r, 400, "request.invalid")
	if errs := r.body["errors"].([]any); errs[0].(map[string]any)["field"] != "deadline" {
		t.Fatalf("errors %v", errs)
	}
	if r := call(t, srv, "POST", "/api/tasks", taskBody+"\n ", "Idempotency-Key", "strict-005"); r.status != 201 {
		t.Fatalf("trailing whitespace: %d %s", r.status, r.raw)
	}
	r = call(t, srv, "POST", "/api/tasks", strings.Replace(taskBody, `"minutes":30`, `"minutes":999`, 1), "Idempotency-Key", "strict-006")
	if r.status != 400 || !strings.Contains(r.raw, "task.minutes_range") {
		t.Fatalf("validation %s", r.raw)
	}
	expectProblem(t, call(t, srv, "POST", "/api/tasks/b@d/start", "", "Idempotency-Key", "strict-007"), 400, "request.invalid")
}

func TestIdempotencyKey(t *testing.T) {
	srv, w := newServer(t, config.ModeDemo)
	expectProblem(t, call(t, srv, "POST", "/api/tasks", taskBody), 400, "idempotency.key_required")
	expectProblem(t, call(t, srv, "POST", "/api/tasks", taskBody, "Idempotency-Key", "short"), 400, "request.invalid")
	first := call(t, srv, "POST", "/api/tasks", taskBody, "Idempotency-Key", "create-0001")
	again := call(t, srv, "POST", "/api/tasks", taskBody, "Idempotency-Key", "create-0001")
	if first.status != 201 || again.raw != first.raw || again.header.Get("Idempotent-Replayed") != "true" || again.header.Get("ETag") != first.header.Get("ETag") {
		t.Fatalf("replay %d %s / %s", again.status, again.raw, first.raw)
	}
	expectProblem(t, call(t, srv, "POST", "/api/tasks", strings.Replace(taskBody, "レポート", "別", 1), "Idempotency-Key", "create-0001"), 409, "idempotency.key_reused")
	if b := w.Account(t, "demo-0123456789abcdef0123456789abcdef").Balance; b != 900 {
		t.Fatalf("balance %d", b)
	}
	// 何も反映せずに失敗した要求は記録せず、同じキーで再試行できる。
	expectProblem(t, call(t, srv, "PUT", "/api/settings", `{"snoozeMinutes":5,"mode":"simple","sound":false,"reducedMotion":false,"grime":false}`, "Idempotency-Key", "retry-0001", "If-Match", `"7"`), 412, "precondition.failed")
	if r := call(t, srv, "PUT", "/api/settings", `{"snoozeMinutes":5,"mode":"simple","sound":false,"reducedMotion":false,"grime":false}`, "Idempotency-Key", "retry-0001", "If-Match", `"0"`); r.status != 200 {
		t.Fatalf("retry after released key: %d %s", r.status, r.raw)
	}
}

func TestIfMatch(t *testing.T) {
	srv, _ := newServer(t, config.ModeDemo)
	body := `{"name":"","context":"","timeZone":"Asia/Tokyo"}`
	expectProblem(t, call(t, srv, "PUT", "/api/profile", body, "Idempotency-Key", "profile-01"), 428, "precondition.required")
	expectProblem(t, call(t, srv, "PUT", "/api/profile", body, "Idempotency-Key", "profile-02", "If-Match", `"3"`), 412, "precondition.failed")
	r := call(t, srv, "PUT", "/api/profile", body, "Idempotency-Key", "profile-03", "If-Match", `"0"`)
	if r.status != 200 || r.header.Get("ETag") != `"1"` {
		t.Fatalf("%d %s %v", r.status, r.raw, r.header)
	}
	expectProblem(t, call(t, srv, "PUT", "/api/profile", `{"name":"","context":"","timeZone":"Nowhere/X"}`, "Idempotency-Key", "profile-04", "If-Match", `"1"`), 400, "request.invalid")
}

func TestAuthAndOrigin(t *testing.T) {
	demo, _ := newServer(t, config.ModeDemo)
	for _, bad := range []string{"0123456789abcdef0123456789abcdef", "Bearer x", "Demo invalid"} {
		expectProblem(t, call(t, demo, "GET", "/api/profile", "", "Authorization", bad), 401, "auth.required")
	}
	r := call(t, demo, "GET", "/api/profile", "", "Authorization", "")
	if r.status != 200 || !strings.Contains(r.header.Get("Set-Cookie"), "dopagaki_demo=") || !strings.Contains(r.header.Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("cookie %v", r.header)
	}
	tok := call(t, demo, "POST", "/api/extension/token", "", "Idempotency-Key", "token-0001")
	if tok.body["authorization"] != demoAuth {
		t.Fatalf("token %s", tok.raw)
	}
	expectProblem(t, call(t, demo, "POST", "/api/shortcut/key", "", "Idempotency-Key", "origin-001", "Origin", "https://untrusted.example"), 403, "origin.forbidden")
	if r := call(t, demo, "POST", "/api/shortcut/key", "", "Idempotency-Key", "origin-002", "Origin", "chrome-extension://zzzz"); r.status != 201 {
		t.Fatalf("demo extension %d", r.status)
	}

	live, _ := newServer(t, config.ModeLive)
	expectProblem(t, call(t, live, "GET", "/api/profile", "", "Authorization", ""), 401, "auth.required")
	expectProblem(t, call(t, live, "GET", "/api/profile", "", "Authorization", "Bearer bad"), 401, "auth.expired")
	if r := call(t, live, "GET", "/api/profile", "", "Authorization", "Bearer good"); r.status != 200 {
		t.Fatalf("live %d", r.status)
	}
	expectProblem(t, call(t, live, "POST", "/api/shortcut/key", "", "Authorization", "Bearer good", "Idempotency-Key", "origin-003", "Origin", "chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), 403, "origin.forbidden")
	if r := call(t, live, "POST", "/api/shortcut/key", "", "Authorization", "Bearer good", "Idempotency-Key", "origin-004", "Origin", "chrome-extension://abcdefghijklmnopabcdefghijklmnop"); r.status != 201 {
		t.Fatalf("allowed extension %d", r.status)
	}
	expectProblem(t, call(t, live, "POST", "/api/extension/token", "", "Authorization", "Bearer good", "Idempotency-Key", "token-0002"), 403, "demo.only")
	if r := call(t, live, "GET", "/api/health", "", "Authorization", ""); r.status != 200 || r.header.Get("Content-Security-Policy") != "default-src 'none'; base-uri 'none'; frame-ancestors 'none'" {
		t.Fatalf("health %d %v", r.status, r.header)
	}
}

func TestEventsStreamSendsInitialRevisions(t *testing.T) {
	srv, w := newServer(t, config.ModeDemo)
	call(t, srv, "POST", "/api/tasks", taskBody, "Idempotency-Key", "events-001")
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL+"/api/events?token="+strings.TrimPrefix(demoAuth, "Demo "), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %s", res.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(res.Body)
	var events []string
	for len(events) < 10 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimSpace(line))
		}
	}
	if events[2] != `data: {"resource":"tasks","revision":1}` || events[4] != `data: {"resource":"wallet","revision":1}` {
		t.Fatalf("events %v", events)
	}
	// 変更すると、そのリソースだけが届く。
	if _, err := w.Shortcut.Issue(context.Background(), "demo-0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			if strings.TrimSpace(line) != `data: {"resource":"shortcut","revision":1}` {
				t.Fatalf("change %s", line)
			}
			return
		}
	}
}

// fakeCalendar は連携の状態だけを持つ Calendar。
type fakeCalendar struct {
	connected   bool
	disconnects int
}

func (f *fakeCalendar) status() oapi.CalendarStatus {
	sync := oapi.CalendarStatusSyncDisconnected
	if f.connected {
		sync = oapi.CalendarStatusSyncSynced
	}
	return oapi.CalendarStatus{Enabled: true, Connected: f.connected, Sync: sync}
}

func (f *fakeCalendar) Status(context.Context, string) (oapi.CalendarStatus, error) {
	return f.status(), nil
}
func (f *fakeCalendar) Connect(context.Context, string, string, string) (string, error) {
	return "https://accounts.example/auth", nil
}
func (f *fakeCalendar) Callback(context.Context, string, string, string) string { return "/" }
func (f *fakeCalendar) SyncNow(context.Context, string) (oapi.CalendarStatus, error) {
	return f.status(), nil
}
func (f *fakeCalendar) Disconnect(context.Context, string) (oapi.CalendarStatus, error) {
	f.disconnects++
	f.connected = false
	return f.status(), nil
}
func (f *fakeCalendar) Webhook(context.Context, string, string, string) error { return nil }
func (f *fakeCalendar) Renew(context.Context) error                           { return nil }

func TestDisconnectCalendar(t *testing.T) {
	// 連携を使わない環境では 503 calendar.unavailable。
	off, _ := newServer(t, config.ModeDemo)
	expectProblem(t, call(t, off, "POST", "/api/calendar/disconnect", "", "Idempotency-Key", "disc-0001"), 503, "calendar.unavailable")

	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	cal := &fakeCalendar{connected: true}
	api, err := apihttp.NewAPI(apihttp.Deps{
		Config: config.Config{Mode: config.ModeDemo, AppURL: "http://app.test"}, Clock: w.Clock, Log: w.Log, Idempotency: w.Idempotency,
		Profiles: w.Profiles, Tasks: w.Tasks, Work: w.Work, Wallet: w.Wallet, Viewing: w.Viewing, Sleep: w.Sleep,
		Records: w.Records, Shortcut: w.Shortcut, Events: realtime.NewHub(w.Store, w.Log), Calendar: cal,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(apihttp.APIOnly(api))
	t.Cleanup(srv.Close)
	expectProblem(t, call(t, srv, "POST", "/api/calendar/disconnect", ""), 400, "idempotency.key_required")
	r := call(t, srv, "POST", "/api/calendar/disconnect", "", "Idempotency-Key", "disc-0002")
	if r.status != 200 || r.body["connected"] != false || r.body["sync"] != "disconnected" || r.body["enabled"] != true || cal.disconnects != 1 {
		t.Fatalf("disconnect: %d %s", r.status, r.raw)
	}
	// 同じキーの再送は記録した応答を返し、解除をやり直さない。
	again := call(t, srv, "POST", "/api/calendar/disconnect", "", "Idempotency-Key", "disc-0002")
	if again.status != 200 || again.raw != r.raw || again.header.Get("Idempotent-Replayed") != "true" || cal.disconnects != 1 {
		t.Fatalf("replay: %d %s", again.status, again.raw)
	}
	// 連携していないときに呼んでも 200。
	if r := call(t, srv, "POST", "/api/calendar/disconnect", "", "Idempotency-Key", "disc-0003"); r.status != 200 || r.body["connected"] != false {
		t.Fatalf("disconnect while disconnected: %d %s", r.status, r.raw)
	}
}
