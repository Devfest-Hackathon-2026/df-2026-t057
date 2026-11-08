package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

type doc struct {
	ID  string `json:"id" firestore:"id"`
	Seq int64  `json:"seq" firestore:"seq"`
}

var ctx = context.Background()

func TestTransactionSemantics(t *testing.T) {
	r := store.NewRunner(memory.NewStore())
	committed := false
	err := r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		for i := range 3 {
			ref := store.UserItem("u", "items", string(rune('a'+i)))
			if err := tx.Set(ref, doc{ID: ref.ID, Seq: int64(i)}); err != nil {
				return err
			}
		}
		got, ok, err := store.Get[doc](ctx, tx, store.UserItem("u", "items", "b"))
		if err != nil || !ok || got.Seq != 1 {
			t.Fatalf("read your write: %+v %v %v", got, ok, err)
		}
		if _, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items")}); !errors.Is(err, store.ErrQueryAfterWrite) {
			t.Fatalf("query after write: %v", err)
		}
		if rev, err := tx.Touch(ctx, "u", store.ResourceTasks); err != nil || rev != 1 {
			t.Fatal(err)
		}
		if rev, _ := tx.Touch(ctx, "u", store.ResourceTasks); rev != 1 {
			t.Fatal("touch twice bumped twice")
		}
		tx.AfterCommit(func() { committed = true })
		return nil
	})
	if err != nil || !committed {
		t.Fatal(err)
	}
	err = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		items, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items"), Filters: []store.Filter{{Field: "seq", Op: store.Lt, Value: int64(2)}}, OrderBy: "seq", Desc: true, Limit: 5})
		if err != nil || len(items) != 2 || items[0].ID != "b" {
			t.Fatalf("query %+v %v", items, err)
		}
		if err := tx.Set(store.UserItem("u", "items", "z"), doc{}); !errors.Is(err, store.ErrReadOnly) {
			t.Fatalf("read only: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// fn がエラーを返すと何も書かない。
	boom := errors.New("boom")
	if err := r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		_ = tx.Set(store.UserItem("u", "items", "x"), doc{ID: "x"})
		return boom
	}); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	_ = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, ok, _ := store.Get[doc](ctx, tx, store.UserItem("u", "items", "x")); ok {
			t.Fatal("rolled back write persisted")
		}
		return nil
	})
}

func TestValidUID(t *testing.T) {
	for uid, want := range map[string]bool{"abc": true, "demo-0f": true, "": false, "a/b": false, "..": false, "__x__": false} {
		if store.ValidUID(uid) != want {
			t.Fatalf("%q", uid)
		}
	}
}
