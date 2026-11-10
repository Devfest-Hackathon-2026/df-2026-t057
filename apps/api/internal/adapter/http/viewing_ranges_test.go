package http_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
)

func TestViewingRangeContractAndReplay(t *testing.T) {
	srv, world := newServer(t, config.ModeDemo)
	start := call(t, srv, http.MethodPost, "/api/viewing/sessions", `{"seconds":300}`, "Origin", "http://app.test", "Idempotency-Key", "range-start")
	if start.status != http.StatusCreated {
		t.Fatalf("start: %s", start.raw)
	}
	session := start.body["session"].(map[string]any)["id"].(string)
	world.Clock.Advance(100 * time.Second)
	first := call(t, srv, http.MethodPost, "/api/viewing/reports", fmt.Sprintf(`{"seconds":10,"sessionId":%q,"fromSeconds":0}`, session), "Origin", "http://app.test", "Idempotency-Key", "range-first")
	if first.status != http.StatusOK || first.body["acceptedSeconds"] != float64(10) {
		t.Fatalf("first: %s", first.raw)
	}
	body := fmt.Sprintf(`{"seconds":54,"sessionId":%q,"fromSeconds":10}`, session)
	second := call(t, srv, http.MethodPost, "/api/viewing/reports", body, "Origin", "http://app.test", "Idempotency-Key", "range-next")
	if second.status != http.StatusOK || second.body["acceptedSeconds"] != float64(54) || second.body["reportCursor"] != float64(64) {
		t.Fatalf("second: %s", second.raw)
	}
	replayed := call(t, srv, http.MethodPost, "/api/viewing/reports", body, "Origin", "http://app.test", "Idempotency-Key", "range-next")
	if replayed.raw != second.raw {
		t.Fatalf("replay changed: %s", replayed.raw)
	}
}
