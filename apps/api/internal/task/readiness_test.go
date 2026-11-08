package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
)

func TestArchiveAndDeletePreserveCompletionAndWork(t *testing.T) {
	w := testkit.New(t, noon)
	created, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "archive-me", Input: input("report", 30, noon.Add(2*time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Work.Start(ctx, uid, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(5 * time.Minute)
	if _, err = w.Work.Complete(ctx, uid, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	before, err := w.Records.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	balance := w.Account(t, uid).Balance
	if err = w.Tasks.Archive(ctx, uid, created.Task.ID, false); err != nil {
		t.Fatal(err)
	}
	list, err := w.Tasks.List(ctx, uid)
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err = w.Runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		_, e := w.Tasks.ApplyImport(ctx, tx, uid, []task.Imported{{TaskID: created.Task.ID, Title: "replayed", Deadline: noon.Add(2 * time.Hour), Minutes: 30}}, w.Clock.Now())
		return e
	}); err != nil {
		t.Fatal(err)
	}
	list, _ = w.Tasks.List(ctx, uid)
	if len(list.Items) != 0 {
		t.Fatal("archive resurrected by import")
	}
	if err = w.Tasks.Archive(ctx, uid, created.Task.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = w.Tasks.Delete(ctx, uid, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	after, err := w.Records.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if before.TotalCompleted != 1 || after.TotalCompleted != 1 || before.TotalEarned != after.TotalEarned || balance != w.Account(t, uid).Balance {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	if len(after.Weeks) != 12 || after.Weeks[11].Starts != 1 || after.Weeks[11].WorkSeconds != 300 {
		t.Fatalf("weeks=%+v", after.Weeks)
	}
}
