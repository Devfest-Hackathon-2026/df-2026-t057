package calendar

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
)

type heldListClient struct {
	Client
	calls   atomic.Int32
	ready   chan struct{}
	release chan struct{}
}

func (c *heldListClient) ListEvents(ctx context.Context, calendarID, token, page string) (EventPage, error) {
	result, err := c.Client.ListEvents(ctx, calendarID, token, page)
	if c.calls.Add(1) == 1 {
		close(c.ready)
		select {
		case <-c.release:
		case <-ctx.Done():
			return EventPage{}, ctx.Err()
		}
	}
	return result, err
}

type fixedClientGoogle struct {
	Google
	client Client
}

func (g fixedClientGoogle) Client(context.Context, string) (Client, error) { return g.client, nil }

func TestOlderConcurrentSyncCannotOverwriteNewerImport(t *testing.T) {
	e := newEnv(t)
	e.connect()
	e.mustDrain()
	old := manualEvent("external-event", "旧版", e.clock.Now().Add(3*time.Hour), 30)
	if _, err := e.cal.InsertEvent(e.ctx, e.connection().CalendarID, old); err != nil {
		t.Fatal(err)
	}
	client := &heldListClient{Client: e.cal, ready: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(client.release) }) }
	t.Cleanup(release)
	e.svc.Google = fixedClientGoogle{Google: e.google, client: client}
	done := make(chan error, 1)
	go func() { done <- e.svc.sync(e.ctx, outbox.Job{UID: uid, ID: "old-sync"}) }()
	select {
	case <-client.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("old sync did not reach list")
	}
	newer := old
	newer.Summary = "新版"
	if _, err := e.cal.UpdateEvent(e.ctx, e.connection().CalendarID, newer); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.sync(e.ctx, outbox.Job{UID: uid, ID: "new-sync"}); err != nil {
		t.Fatal(err)
	}
	token := e.connection().SyncToken
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items := e.list().Items
	if len(items) != 1 || items[0].Title != "新版" {
		t.Fatalf("newer import overwritten: %+v", items)
	}
	if e.connection().SyncToken != token {
		t.Fatal("sync token regressed")
	}
}
