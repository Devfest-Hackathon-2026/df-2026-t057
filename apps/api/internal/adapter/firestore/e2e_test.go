package firestore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gfirestore "cloud.google.com/go/firestore"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/firestore"
	apihttp "github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/realtime"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// emulatorWorld はエミュレーターの Firestore を保存先にした World と、その上の API。利用者は実行ごとに別にする。
func emulatorWorld(t *testing.T) (*testkit.World, *httptest.Server, string) {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip(firestore.ErrNoEmulator)
	}
	ctx := context.Background()
	client, err := gfirestore.NewClient(ctx, "demo-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	backend := firestore.New(client)
	w := testkit.NewWithBackend(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST), backend)
	api, err := apihttp.NewAPI(apihttp.Deps{
		Config: config.Config{Mode: config.ModeDemo, AppURL: "http://app.test"}, Clock: w.Clock, Log: w.Log, Idempotency: w.Idempotency,
		Profiles: w.Profiles, Tasks: w.Tasks, Work: w.Work, Wallet: w.Wallet, Viewing: w.Viewing, Sleep: w.Sleep,
		Records: w.Records, Shortcut: w.Shortcut, Events: realtime.NewHub(backend, w.Log),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(apihttp.APIOnly(api))
	t.Cleanup(srv.Close)
	var b [16]byte
	_, _ = rand.Read(b[:])
	return w, srv, hex.EncodeToString(b[:])
}

type reply struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func send(t *testing.T, srv *httptest.Server, token, method, path, body string, headers ...string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Demo "+token)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	r := reply{status: res.StatusCode, header: res.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &r.body)
	return r
}

func count(entries []wallet.Entry, kind wallet.Kind) int {
	n := 0
	for _, e := range entries {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// TestFirestoreEndToEnd は、実際の Firestore（エミュレーター）の上で、課題の登録・作業の記録・視聴の報告・朝の精算と
// Idempotency-Key の再送を通し、口座が台帳から作り直した結果と一致することを確かめる。
func TestFirestoreEndToEnd(t *testing.T) {
	w, srv, token := emulatorWorld(t)
	uid := "demo-" + token
	ctx := context.Background()

	// 課題の登録と、同じキーの再送（記録した応答を返し、登録の報酬は1回）。
	taskBody := fmt.Sprintf(`{"title":"レポート","notes":"","why":"","deadline":%q,"minutes":30,"url":""}`, time.Date(2026, 11, 8, 18, 0, 0, 0, testkit.JST).Format(time.RFC3339))
	first := send(t, srv, token, "POST", "/api/tasks", taskBody, "Idempotency-Key", "e2e-create-1")
	if first.status != 201 {
		t.Fatalf("create: %d %s", first.status, first.raw)
	}
	again := send(t, srv, token, "POST", "/api/tasks", taskBody, "Idempotency-Key", "e2e-create-1")
	if again.status != 201 || again.raw != first.raw || again.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", again.status, again.raw)
	}
	if n := count(w.Entries(t, uid), wallet.KindRegistration); n != 1 {
		t.Fatalf("registration entries: %d", n)
	}
	taskID, _ := first.body["id"].(string)

	// 作業: 開始 → 20分 → 記録（同じキーの再送は報酬を二重にしない）。
	start := send(t, srv, token, "POST", "/api/tasks/"+taskID+"/start", "", "Idempotency-Key", "e2e-start-01")
	if start.status != 200 {
		t.Fatalf("start: %d %s", start.status, start.raw)
	}
	sessionID, _ := start.body["id"].(string)
	w.Clock.Advance(20 * time.Minute)
	confirmBody := fmt.Sprintf(`{"sessionId":%q}`, sessionID)
	confirm := send(t, srv, token, "POST", "/api/work-session/confirm", confirmBody, "Idempotency-Key", "e2e-confirm-1")
	if confirm.status != 200 {
		t.Fatalf("confirm: %d %s", confirm.status, confirm.raw)
	}
	if r := send(t, srv, token, "POST", "/api/work-session/confirm", confirmBody, "Idempotency-Key", "e2e-confirm-1"); r.status != 200 || r.raw != confirm.raw {
		t.Fatalf("confirm replay: %d %s", r.status, r.raw)
	}
	if n := count(w.Entries(t, uid), wallet.KindWork); n != 1 {
		t.Fatalf("work entries: %d", n)
	}
	w.CheckProjection(t, uid)

	// 視聴: 区切りを始めて報告する（領収書ごとに1回）。
	view := send(t, srv, token, "POST", "/api/viewing/sessions", `{"seconds":300}`, "Idempotency-Key", "e2e-view-0001")
	if view.status != 201 {
		t.Fatalf("start viewing: %d %s", view.status, view.raw)
	}
	w.Clock.Advance(30 * time.Second)
	reportBody := `{"seconds":20,"sessionId":"e2e-view-0001"}`
	before := w.Account(t, uid).Balance
	if r := send(t, srv, token, "POST", "/api/viewing/reports", reportBody, "Idempotency-Key", "e2e-report-01"); r.status != 200 {
		t.Fatalf("report: %d %s", r.status, r.raw)
	}
	if r := send(t, srv, token, "POST", "/api/viewing/reports", reportBody, "Idempotency-Key", "e2e-report-01"); r.status != 200 || r.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("report replay: %d %s", r.status, r.raw)
	}
	if after := w.Account(t, uid).Balance; before-after != 20 {
		t.Fatalf("viewing consumed %d, want 20", before-after)
	}
	w.CheckProjection(t, uid)

	// 朝の精算: 就寝の設定をして夜に登録した報酬は保留になり、朝に解放と加算をする。
	if _, err := w.Sleep.Update(ctx, uid, sleep.Schedule{Bedtime: "23:30", Wake: "07:00"}, 0); err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(time.Date(2026, 11, 8, 23, 45, 0, 0, testkit.JST))
	w.CreateTask(t, uid, "夜の課題", 30, time.Date(2026, 11, 20, 0, 0, 0, 0, testkit.JST))
	if a := w.Account(t, uid); a.Deferred != 300 {
		t.Fatalf("night reward not deferred: %+v", a)
	}
	w.Clock.Set(time.Date(2026, 11, 9, 7, 30, 0, 0, testkit.JST))
	morning := send(t, srv, token, "POST", "/api/sleep/morning", "", "Idempotency-Key", "e2e-morning-1")
	if morning.status != 200 || morning.body["morning"] == nil {
		t.Fatalf("morning: %d %s", morning.status, morning.raw)
	}
	if r := send(t, srv, token, "POST", "/api/sleep/morning", "", "Idempotency-Key", "e2e-morning-1"); r.status != 200 || r.raw != morning.raw {
		t.Fatalf("morning replay: %d %s", r.status, r.raw)
	}
	entries := w.Entries(t, uid)
	if count(entries, wallet.KindSleep) != 1 || count(entries, wallet.KindRelease) != 1 {
		t.Fatalf("morning entries: %+v", entries)
	}
	if a := w.Account(t, uid); a.Deferred != 0 {
		t.Fatalf("deferred not released: %+v", a)
	}
	w.CheckProjection(t, uid)

	// 台帳の検索（並べ替え・範囲）と、記録の集計が Firestore で動く。
	ledger := send(t, srv, token, "GET", "/api/wallet/ledger?limit=3", "")
	if ledger.status != 200 || len(ledger.body["entries"].([]any)) != 3 || ledger.body["next"] == nil {
		t.Fatalf("ledger: %d %s", ledger.status, ledger.raw)
	}
	if r := send(t, srv, token, "GET", "/api/records", ""); r.status != 200 {
		t.Fatalf("records: %d %s", r.status, r.raw)
	}
}
