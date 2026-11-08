package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/realtime"
)

// HeartbeatInterval はコメント行を送って接続を保つ間隔。
const HeartbeatInterval = 15 * time.Second

// eventsHandler は GET /api/events（Server-Sent Events）。接続直後にすべてのリソースの版を送り、以後は変わった版だけを送る。
func eventsHandler(hub *realtime.Hub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		initial, updates, cancel, err := hub.Subscribe(ctx, uidFrom(ctx))
		if err != nil {
			return
		}
		defer cancel()
		rc := http.NewResponseController(w)
		// サーバーの WriteTimeout で長い接続が切れないようにする。
		_ = rc.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		send := func(changes []realtime.Change) error {
			for _, c := range changes {
				data, _ := json.Marshal(oapi.ChangeEvent{Resource: oapi.Resource(c.Resource), Revision: int(c.Revision)})
				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					return err //nolint:wrapcheck // 接続が切れただけ。
				}
			}
			return rc.Flush() //nolint:wrapcheck // 接続が切れただけ。
		}
		if err := send(initial); err != nil {
			return
		}
		ticker := time.NewTicker(HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case changes, ok := <-updates:
				if !ok || send(changes) != nil {
					return
				}
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			}
		}
	})
}
