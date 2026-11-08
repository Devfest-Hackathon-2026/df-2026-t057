package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/idempotency"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/realtime"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

const (
	demoUID        = "demo-0123456789abcdef0123456789abcdef"
	demoAuthHeader = "Demo 0123456789abcdef0123456789abcdef"
)

func demoServer(t *testing.T) (*httptest.Server, *testkit.World) {
	t.Helper()
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	api, err := NewAPI(Deps{
		Config: config.Config{Mode: config.ModeDemo, AppURL: "http://app.test"}, Clock: w.Clock, Log: w.Log, Idempotency: w.Idempotency,
		Profiles: w.Profiles, Tasks: w.Tasks, Work: w.Work, Wallet: w.Wallet, Viewing: w.Viewing, Sleep: w.Sleep,
		Records: w.Records, Shortcut: w.Shortcut, Events: realtime.NewHub(w.Store, w.Log),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(APIOnly(api))
	t.Cleanup(srv.Close)
	return srv, w
}

func post(t *testing.T, srv *httptest.Server, path, body, key string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", demoAuthHeader)
	req.Header.Set("Idempotency-Key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(raw)
}

func registrations(t *testing.T, w *testkit.World) int {
	t.Helper()
	n := 0
	for _, e := range w.Entries(t, demoUID) {
		if e.Kind == wallet.KindRegistration {
			n++
		}
	}
	return n
}

// TestRetryAfterCrashAppliesExactlyOnce は、処理の途中で止まった要求（記録は処理中のまま）を、
// Lease が切れた後の再送が最初から処理して正しい応答を返し、確定した後の再送は記録した応答を返すことを確かめる。
func TestRetryAfterCrashAppliesExactlyOnce(t *testing.T) {
	srv, w := demoServer(t)
	body := `{"title":"レポート","notes":"","why":"","deadline":"2026-11-08T18:00:00+09:00","minutes":30,"url":""}`
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://app.test/api/tasks", nil)
	hash := requestHash(req, []byte(body))

	// 前の処理が記録を「処理中」にしたまま止まった（確定の前に止まったので、何も反映されていない）。
	if _, _, err := w.Idempotency.Begin(context.Background(), demoUID, "crash-0001", hash); err != nil {
		t.Fatal(err)
	}
	if status, _, raw := post(t, srv, "/api/tasks", body, "crash-0001"); status != 409 || !strings.Contains(raw, "idempotency.in_progress") {
		t.Fatalf("within lease: %d %s", status, raw)
	}
	w.Clock.Advance(idempotency.Lease + time.Second)
	status, header, first := post(t, srv, "/api/tasks", body, "crash-0001")
	if status != 201 || header.Get("Idempotent-Replayed") != "" {
		t.Fatalf("retry after lease: %d %s", status, first)
	}
	var created map[string]any
	_ = json.Unmarshal([]byte(first), &created)
	if created["title"] != "レポート" || registrations(t, w) != 1 {
		t.Fatalf("retry did not apply once: %s entries=%d", first, registrations(t, w))
	}
	w.CheckProjection(t, demoUID)

	// 確定した要求の再送（応答が届かなかった場合）は、同じ応答を返し、何も反映しない。
	for range 2 {
		status, header, again := post(t, srv, "/api/tasks", body, "crash-0001")
		if status != 201 || again != first || header.Get("Idempotent-Replayed") != "true" {
			t.Fatalf("replay: %d %s", status, again)
		}
	}
	if registrations(t, w) != 1 {
		t.Fatalf("replay applied again: %d", registrations(t, w))
	}
	list, _ := w.Tasks.List(context.Background(), demoUID)
	if len(list.Items) != 1 {
		t.Fatalf("tasks: %d", len(list.Items))
	}
}

// TestRequestCommitsStateAndResponseTogether は、要求の中で複数回トランザクションを使う操作（完了: 作業の記録と課題の読み直し）でも
// 1つの応答に矛盾がなく、再送で報酬が二重にならないことを確かめる。
func TestRequestCommitsStateAndResponseTogether(t *testing.T) {
	srv, w := demoServer(t)
	body := `{"title":"レポート","notes":"","why":"","deadline":"2026-11-08T18:00:00+09:00","minutes":30,"url":""}`
	_, _, raw := post(t, srv, "/api/tasks", body, "create-0001")
	var created map[string]any
	_ = json.Unmarshal([]byte(raw), &created)
	taskID, _ := created["id"].(string)
	if status, _, raw := post(t, srv, "/api/tasks/"+taskID+"/start", "", "start-0001"); status != 200 {
		t.Fatalf("start: %d %s", status, raw)
	}
	w.Clock.Advance(20 * time.Minute)
	status, _, first := post(t, srv, "/api/tasks/"+taskID+"/complete", "", "complete-01")
	if status != 200 || !strings.Contains(first, `"status":"completed"`) || !strings.Contains(first, `"work"`) {
		t.Fatalf("complete: %d %s", status, first)
	}
	entries := len(w.Entries(t, demoUID))
	w.CheckProjection(t, demoUID)
	if status, _, again := post(t, srv, "/api/tasks/"+taskID+"/complete", "", "complete-01"); status != 200 || again != first {
		t.Fatalf("complete replay: %d %s", status, again)
	}
	if len(w.Entries(t, demoUID)) != entries {
		t.Fatal("replay added ledger entries")
	}
}

// TestFailedRequestCommitsNothing は、4xx の応答では要求の中の書き込みを何も確定せず、同じキーで再試行できることを確かめる。
func TestFailedRequestCommitsNothing(t *testing.T) {
	srv, w := demoServer(t)
	// 課題の件数の上限を超える登録: 先に 200 件登録してから 201 件目を試す。
	body := `{"title":"レポート","notes":"","why":"","deadline":"2026-11-08T18:00:00+09:00","minutes":30,"url":""}`
	for i := range 200 {
		id := fmt.Sprintf("t-%03d", i)
		if status, _, raw := post(t, srv, "/api/tasks", strings.Replace(body, `{"title"`, `{"id":"`+id+`","title"`, 1), "fill-"+id); status != 201 {
			t.Fatalf("fill %d: %d %s", i, status, raw)
		}
	}
	rev := len(w.Entries(t, demoUID))
	status, _, raw := post(t, srv, "/api/tasks", body, "over-00001")
	if status != 409 || !strings.Contains(raw, "task.limit_reached") {
		t.Fatalf("over the limit: %d %s", status, raw)
	}
	if len(w.Entries(t, demoUID)) != rev {
		t.Fatal("failed request wrote ledger entries")
	}
	// 記録は残らないので、同じキーでやり直せる（記録があれば in_progress か replay になる）。
	if status, _, raw := post(t, srv, "/api/tasks", body, "over-00001"); status != 409 || !strings.Contains(raw, "task.limit_reached") {
		t.Fatalf("retry after failure: %d %s", status, raw)
	}
}
