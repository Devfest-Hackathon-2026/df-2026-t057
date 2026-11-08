package firestore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/account"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

func TestFirestoreReadinessDeletionAndHistory(t *testing.T) {
	w, _, token := emulatorWorld(t)
	ctx := context.Background()
	uid := "cleanup-" + token
	data, ok := w.Backend.(account.Data)
	if !ok {
		t.Fatal("missing data adapter")
	}
	s := account.Service{Runner: w.Runner, Clock: w.Clock, IDs: id.Random{}, Data: data, Enabled: true}
	created, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "archive", Input: task.Input{Title: "synthetic", Minutes: 30, Deadline: w.Clock.Now().Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Work.Complete(ctx, uid, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	if err = w.Tasks.Archive(ctx, uid, created.Task.ID, false); err != nil {
		t.Fatal(err)
	}
	records, err := w.Records.Get(ctx, uid)
	if err != nil || records.TotalCompleted != 1 {
		t.Fatalf("records=%v err=%v", records, err)
	}
	// An unknown nested collection and >100 rows must also be cleaned.
	err = w.Runner.Update(ctx, func(_ context.Context, tx *store.Tx) error {
		for i := range 105 {
			if err := tx.Set(store.UserItem(uid, "extra", fmt.Sprintf("row-%03d", i)), map[string]string{"synthetic": "true"}); err != nil {
				return err
			}
		}
		return tx.Set(store.Ref{Collection: "users/" + uid + "/extra/row-000/nested", ID: "child"}, map[string]string{"synthetic": "true"})
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Begin(ctx, uid, w.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A separate Runner/transaction must observe the fence.
	other := store.NewRunner(w.Backend)
	err = other.Update(ctx, func(_ context.Context, tx *store.Tx) error {
		return tx.Set(store.UserDoc(uid, "profile"), map[string]string{"name": "late"})
	})
	if err == nil {
		t.Fatal("separate writer bypassed deletion fence")
	}
	w.Clock.Advance(91 * time.Second)
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if status, err := s.Status(ctx, receipt); err != nil || status != account.Complete {
		t.Fatalf("status=%v err=%v", status, err)
	}
	exported, err := data.Export(ctx, uid)
	if err != nil || len(exported["archivedTasks"]) != 0 || len(exported["completionRecords"]) != 0 {
		t.Fatalf("leftovers=%v err=%v", exported, err)
	}
	if err = other.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		rows, err := store.List[map[string]any](ctx, tx, store.Query{Collection: "users/" + uid + "/extra"})
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatal("unknown collection left")
		}
		rows, err = store.List[map[string]any](ctx, tx, store.Query{Collection: "users/" + uid + "/extra/row-000/nested"})
		if len(rows) != 0 {
			t.Fatal("nested collection left")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
