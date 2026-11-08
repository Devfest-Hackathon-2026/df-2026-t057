package firestore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	gfirestore "cloud.google.com/go/firestore"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/firestore"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// エミュレーターがあるときだけ実行する（FIRESTORE_EMULATOR_HOST）。
func TestFirestoreBackend(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip(firestore.ErrNoEmulator)
	}
	ctx := context.Background()
	client, err := gfirestore.NewClient(ctx, "demo-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	r := store.NewRunner(firestore.New(client))
	type item struct {
		ID  string `json:"id" firestore:"id"`
		Seq int64  `json:"seq" firestore:"seq"`
	}
	// エミュレーターは実行をまたいでデータを残すので、利用者を実行ごとに変える。
	var suffix [8]byte
	_, _ = rand.Read(suffix[:])
	uid := "emulator-" + hex.EncodeToString(suffix[:])
	err = r.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		for i := range 3 {
			id := string(rune('a' + i))
			if err := tx.Set(store.UserItem(uid, "items", id), item{ID: id, Seq: int64(i)}); err != nil {
				return err
			}
		}
		_, err := tx.Touch(ctx, uid, store.ResourceTasks)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		items, err := store.List[item](ctx, tx, store.Query{Collection: store.UserCollection(uid, "items"), Filters: []store.Filter{{Field: "seq", Op: store.Lt, Value: int64(2)}}, OrderBy: "seq", Desc: true})
		if err != nil || len(items) != 2 || items[0].ID != "b" {
			t.Fatalf("%+v %v", items, err)
		}
		rev, err := tx.Revision(ctx, uid, store.ResourceTasks)
		if rev != 1 {
			t.Fatalf("rev %d", rev)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
