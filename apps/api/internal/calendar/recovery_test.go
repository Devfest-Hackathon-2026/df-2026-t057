package calendar

import (
	"errors"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

func TestConflictRequiresFreshPreviewAndPreservesReward(t *testing.T) {
	for _, choice := range []string{"app", "google"} {
		t.Run(choice, func(t *testing.T) {
			e := newEnv(t)
			e.connect()
			e.mustDrain()
			original := e.createTask("original", 30, e.clock.Now().Add(3*time.Hour))
			e.mustDrain()
			end := original.Deadline
			if _, err := e.tasks.Update(e.ctx, uid, original.ID, task.Input{Title: "local", Minutes: 30, Deadline: end}, e.revision(store.ResourceTasks)); err != nil {
				t.Fatal(err)
			}
			e.cal.userEdit(EventID(uid, original.ID), func(ev *Event) { ev.Summary = "remote" })
			e.mustDrain()
			items, err := e.svc.Conflicts(e.ctx, uid)
			if err != nil || len(items) != 1 {
				t.Fatalf("conflicts=%v err=%v", items, err)
			}
			c := items[0]
			if ev, _ := e.cal.event(EventID(uid, original.ID)); ev.Summary != "remote" {
				t.Fatal("remote overwritten before confirmation")
			}
			if err = e.svc.Resolve(e.ctx, uid, original.ID, choice, c.Remote.ETag, c.Revision-1); !errors.Is(err, apperr.PreconditionFailed("precondition.failed")) {
				t.Fatalf("stale preview=%v", err)
			}
			if err = e.svc.Resolve(e.ctx, uid, original.ID, choice, c.Remote.ETag, c.Revision); err != nil {
				t.Fatal(err)
			}
			// Google is edited again after preview. Resolution must return to review.
			e.cal.userEdit(EventID(uid, original.ID), func(ev *Event) { ev.Summary = "new remote" })
			e.mustDrain()
			items, err = e.svc.Conflicts(e.ctx, uid)
			if err != nil || len(items) != 1 || items[0].Resolution != "" || items[0].Remote.Summary != "new remote" {
				t.Fatalf("refreshed=%v err=%v", items, err)
			}
			c = items[0]
			if err = e.svc.Resolve(e.ctx, uid, original.ID, choice, c.Remote.ETag, c.Revision); err != nil {
				t.Fatal(err)
			}
			e.mustDrain()
			items, err = e.svc.Conflicts(e.ctx, uid)
			if err != nil || len(items) != 0 {
				t.Fatalf("remaining=%v err=%v", items, err)
			}
			expected := "local"
			if choice == "google" {
				expected = "new remote"
			}
			ev, _ := e.cal.event(EventID(uid, original.ID))
			if ev.Summary != expected || e.list().Items[0].Title != expected {
				t.Fatalf("event=%+v tasks=%+v", ev, e.list())
			}
			if e.rewards.granted[original.ID] != 300 {
				t.Fatal("resolution granted registration reward twice")
			}
		})
	}
}

func TestAppResolutionRecoversAppliedPatchAndDeletedEvent(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "applied-before-response", true: "deleted-event"}[deleted], func(t *testing.T) {
			e := newEnv(t)
			e.connect()
			e.mustDrain()
			original := e.createTask("original", 30, e.clock.Now().Add(3*time.Hour))
			e.mustDrain()
			if _, err := e.tasks.Update(e.ctx, uid, original.ID, task.Input{Title: "local", Minutes: 30, Deadline: original.Deadline}, e.revision(store.ResourceTasks)); err != nil {
				t.Fatal(err)
			}
			e.cal.userEdit(EventID(uid, original.ID), func(ev *Event) {
				ev.Summary = "remote"
				if deleted {
					ev.Status = EventCancelled
				}
			})
			if _, err := e.svc.SyncNow(e.ctx, uid); err != nil {
				t.Fatal(err)
			}
			e.mustDrain()
			conflicts, err := e.svc.Conflicts(e.ctx, uid)
			if err != nil || len(conflicts) != 1 {
				t.Fatalf("conflicts=%v err=%v", conflicts, err)
			}
			c := conflicts[0]
			if err = e.svc.Resolve(e.ctx, uid, original.ID, "app", c.Remote.ETag, c.Revision); err != nil {
				t.Fatal(err)
			}
			if !deleted {
				if _, err = e.cal.PatchEvent(e.ctx, e.connection().CalendarID, c.Remote.ID, c.Remote.ETag, ContentPatch(c.Task, time.UTC)); err != nil {
					t.Fatal(err)
				}
			}
			e.mustDrain()
			conflicts, err = e.svc.Conflicts(e.ctx, uid)
			if err != nil || len(conflicts) != 0 {
				t.Fatalf("remaining=%v err=%v", conflicts, err)
			}
			link, ok := e.link(original.ID)
			if !ok {
				t.Fatal("missing resolved link")
			}
			ev, ok := e.cal.event(link.EventID)
			if !ok || ev.Summary != "local" || ev.Status == EventCancelled {
				t.Fatalf("event=%+v", ev)
			}
			if deleted && link.EventID == c.Remote.ID {
				t.Fatal("deleted Google event id reused")
			}
		})
	}
}
