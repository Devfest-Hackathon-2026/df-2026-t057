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
		// 溜めた書き込みは、同じトランザクションの検索にも反映される。
		items, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items"), OrderBy: "seq", Desc: true, Limit: 2})
		if err != nil || len(items) != 2 || items[0].ID != "c" || items[1].ID != "b" {
			t.Fatalf("query after write: %+v %v", items, err)
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

func TestQueryMergesPendingWrites(t *testing.T) {
	r := store.NewRunner(memory.NewStore())
	seed := func(ctx context.Context, tx *store.Tx) error {
		for i := range 4 {
			ref := store.UserItem("u", "items", string(rune('a'+i)))
			if err := tx.Set(ref, doc{ID: ref.ID, Seq: int64(i)}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := r.Update(ctx, seed); err != nil {
		t.Fatal(err)
	}
	err := r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		// b を消し、c を書き換え、e を加える。a はそのまま。
		if err := tx.Delete(store.UserItem("u", "items", "b")); err != nil {
			return err
		}
		if err := tx.Set(store.UserItem("u", "items", "c"), doc{ID: "c", Seq: 10}); err != nil {
			return err
		}
		if err := tx.Set(store.UserItem("u", "items", "e"), doc{ID: "e", Seq: 5}); err != nil {
			return err
		}
		all, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items"), OrderBy: "seq"})
		if err != nil {
			return err
		}
		if got := ids(all); got != "a,d,e,c" {
			t.Fatalf("merged order: %s", got)
		}
		// 上限は重ねた後に適用する（実装の上限で e・c が落ちない）。
		top, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items"), OrderBy: "seq", Desc: true, Limit: 2})
		if err != nil {
			return err
		}
		if got := ids(top); got != "c,e" {
			t.Fatalf("merged limit: %s", got)
		}
		// 条件は溜めた書き込みにも適用する。
		some, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items"), Filters: []store.Filter{{Field: "seq", Op: store.Ge, Value: int64(5)}}, OrderBy: "seq"})
		if err != nil {
			return err
		}
		if got := ids(some); got != "e,c" {
			t.Fatalf("merged filter: %s", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func ids(items []doc) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ","
		}
		out += it.ID
	}
	return out
}

// TestRequestTransaction は要求ごとのトランザクションの約束を確かめる: 中の Update は加わり、失敗した Update の分だけ取り消し、
// View は溜めた書き込みを見て、fn が終わるまで何も確定しない。
func TestRequestTransaction(t *testing.T) {
	mem := memory.NewStore()
	r := store.NewRunner(mem)
	boom := errors.New("boom")
	after := 0
	err := r.Request(ctx, "u", func(ctx context.Context, _ *store.Tx) error {
		if err := r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
			tx.AfterCommit(func() { after++ })
			if _, err := tx.Touch(ctx, "u", store.ResourceTasks); err != nil {
				return err
			}
			return tx.Set(store.UserItem("u", "items", "a"), doc{ID: "a", Seq: 1})
		}); err != nil {
			return err
		}
		// 失敗した Update の書き込みと版は取り消す。
		err := r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
			if _, err := tx.Touch(ctx, "u", store.ResourceWallet); err != nil {
				return err
			}
			_ = tx.Set(store.UserItem("u", "items", "b"), doc{ID: "b", Seq: 2})
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("update error: %v", err)
		}
		if after != 0 {
			t.Fatal("after-commit ran before the request committed")
		}
		// まだ何も確定していない。
		var outside []doc
		if err := store.NewRunner(mem).View(store.Detach(ctx), func(ctx context.Context, tx *store.Tx) error {
			var err error
			outside, err = store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items")})
			return err
		}); err != nil || len(outside) != 0 {
			t.Fatalf("committed early: %+v %v", outside, err)
		}
		// 中の View は溜めた書き込みを見る。
		return r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
			items, err := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items"), OrderBy: "seq"})
			if err != nil || ids(items) != "a" {
				t.Fatalf("view inside request: %+v %v", items, err)
			}
			meta, err := tx.Meta(ctx, "u")
			if err != nil || meta.Revision(store.ResourceTasks) != 1 || meta.Revision(store.ResourceWallet) != 0 {
				t.Fatalf("meta inside request: %+v %v", meta, err)
			}
			return nil
		})
	})
	if err != nil || after != 1 {
		t.Fatalf("request: %v after=%d", err, after)
	}
	_ = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		items, _ := store.List[doc](ctx, tx, store.Query{Collection: store.UserCollection("u", "items")})
		if ids(items) != "a" {
			t.Fatalf("after request: %+v", items)
		}
		return nil
	})

	// fn がエラーを返すと、中の Update の分も含めて何も確定しない。
	err = r.Request(ctx, "u", func(ctx context.Context, _ *store.Tx) error {
		_ = r.Update(ctx, func(_ context.Context, tx *store.Tx) error {
			return tx.Set(store.UserItem("u", "items", "z"), doc{ID: "z"})
		})
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	_ = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, ok, _ := store.Get[doc](ctx, tx, store.UserItem("u", "items", "z")); ok {
			t.Fatal("failed request persisted")
		}
		return nil
	})

	// Detach した Update はその場で確定する。
	err = r.Request(ctx, "u", func(ctx context.Context, _ *store.Tx) error {
		if err := r.Update(store.Detach(ctx), func(_ context.Context, tx *store.Tx) error {
			return tx.Set(store.GlobalDoc("counters", "g"), doc{ID: "g", Seq: 1})
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	_ = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, ok, _ := store.Get[doc](ctx, tx, store.GlobalDoc("counters", "g")); !ok {
			t.Fatal("detached update not committed")
		}
		return nil
	})
}

// TestRequestRetriesOnConflict は、要求の間に別の書き込みで読んだ内容が変わったら、fn をやり直すことを確かめる。
func TestRequestRetriesOnConflict(t *testing.T) {
	mem := memory.NewStore()
	r := store.NewRunner(mem)
	attempts := 0
	err := r.Request(ctx, "u", func(ctx context.Context, _ *store.Tx) error {
		attempts++
		return r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
			got, _, err := store.Get[doc](ctx, tx, store.UserItem("u", "items", "a"))
			if err != nil {
				return err
			}
			if attempts == 1 {
				// 読んだ後、確定する前に別の要求が同じドキュメントを書く。
				if err := store.NewRunner(mem).Update(store.Detach(ctx), func(_ context.Context, tx *store.Tx) error {
					return tx.Set(store.UserItem("u", "items", "a"), doc{ID: "a", Seq: 100})
				}); err != nil {
					return err
				}
			}
			return tx.Set(store.UserItem("u", "items", "a"), doc{ID: "a", Seq: got.Seq + 1})
		})
	})
	if err != nil || attempts != 2 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
	_ = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if got, _, _ := store.Get[doc](ctx, tx, store.UserItem("u", "items", "a")); got.Seq != 101 {
			t.Fatalf("lost update: %+v", got)
		}
		return nil
	})
}
