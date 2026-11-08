package shortcut_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
)

const uid = "u1"

var ctx = context.Background()

func TestShortcutKeyLifecycle(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST)
	w := testkit.New(t, now)
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "t1", Input: task.Input{Title: "レポート", Why: "単位のため", Minutes: 30, Deadline: now.Add(3 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	key, err := w.Shortcut.Issue(ctx, uid)
	if err != nil || !regexp.MustCompile(`^[\w-]+\.[a-f0-9]{64}$`).MatchString(key) {
		t.Fatalf("key %q %v", key, err)
	}
	n, err := w.Shortcut.Open(ctx, key)
	if err != nil || !strings.Contains(n.Message, "単位のため") || n.URL != "http://app.test/?notification=t1" || !strings.Contains(n.Title, "DOPAGAKI") {
		t.Fatalf("%+v %v", n, err)
	}
	err = w.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		today, week, configured, err := w.Shortcut.AppOpenCounts(ctx, tx, uid, w.Clock.Now())
		if today != 1 || week != 1 || !configured {
			t.Fatalf("counts %d %d %v", today, week, configured)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(key, "") && strings.Contains(string(mustKeyDoc(t, w)), key[strings.IndexByte(key, '.')+1:]) {
		t.Fatal("secret stored")
	}
	replaced, _ := w.Shortcut.Issue(ctx, uid)
	if _, err := w.Shortcut.Open(ctx, key); !errors.Is(err, apperr.Unauthenticated("shortcut.key_invalid")) {
		t.Fatalf("old key = %v", err)
	}
	if err := w.Shortcut.Revoke(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Shortcut.Open(ctx, replaced); !errors.Is(err, apperr.Unauthenticated("shortcut.key_invalid")) {
		t.Fatalf("revoked = %v", err)
	}
	if _, err := w.Shortcut.Open(ctx, "bad"); !errors.Is(err, apperr.Unauthenticated("shortcut.key_malformed")) {
		t.Fatalf("malformed = %v", err)
	}
}

func mustKeyDoc(t *testing.T, w *testkit.World) []byte {
	t.Helper()
	var raw map[string]any
	err := w.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		_, err := tx.Get(ctx, store.UserDoc(uid, "shortcut"), &raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := raw["keyHash"].(string)
	return []byte(h)
}

func TestShortcutRateLimitPerAccount(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	key, _ := w.Shortcut.Issue(ctx, uid)
	for i := range 10 {
		if _, err := w.Shortcut.Open(ctx, key); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := w.Shortcut.Open(ctx, key); !errors.Is(err, apperr.RateLimited("shortcut.rate_limited", 0)) {
		t.Fatalf("11th = %v", err)
	}
}

func TestUnknownAccountIsNotCreated(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	key, _ := w.Shortcut.Issue(ctx, uid)
	other := "dTk." + key[strings.IndexByte(key, '.')+1:]
	if _, err := w.Shortcut.Open(ctx, other); !errors.Is(err, apperr.Unauthenticated("shortcut.key_invalid")) {
		t.Fatalf("err = %v", err)
	}
	var meta store.Meta
	_ = w.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		meta, err = tx.Meta(ctx, "u9")
		return err
	})
	if len(meta.Revisions) != 0 {
		t.Fatal("account created")
	}
}
