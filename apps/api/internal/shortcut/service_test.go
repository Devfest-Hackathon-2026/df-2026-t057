package shortcut_test

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
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

func viewAll(t *testing.T, w *testkit.World, seconds int) {
	t.Helper()
	err := w.Runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		_, err := w.Wallet.RecordViewing(ctx, tx, uid, "r1", seconds, w.Clock.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenTellsTimeLeftAndStopsAtZero(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST)
	w := testkit.New(t, now)
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "t1", Input: task.Input{Title: "レポート", Minutes: 30, Deadline: now.Add(3 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	key, _ := w.Shortcut.Issue(ctx, uid)
	// 初めの付与と、課題を登録した付与の合計。
	account, _, err := w.Wallet.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	left := account.Available()
	n, err := w.Shortcut.Open(ctx, key)
	if err != nil || n.Stop || n.MinutesLeft == nil || *n.MinutesLeft != left/60 || !strings.HasPrefix(n.Message, "今日あと"+strconv.Itoa(left/60)+"分。") {
		t.Fatalf("first open %+v %v", n, err)
	}
	viewAll(t, w, left)
	n, err = w.Shortcut.Open(ctx, key)
	if err != nil || !n.Stop || n.MinutesLeft == nil || *n.MinutesLeft != 0 || strings.Contains(n.Message, "今日あと") || n.URL != "http://app.test/?notification=t1" {
		t.Fatalf("zero %+v %v", n, err)
	}
	_, rev, err := w.Wallet.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeSelf, 1800, rev); err != nil {
		t.Fatal(err)
	}
	n, err = w.Shortcut.Open(ctx, key)
	if err != nil || n.Stop || n.MinutesLeft != nil || strings.Contains(n.Message, "今日あと") {
		t.Fatalf("self %+v %v", n, err)
	}
}

func TestCloseRecordsUseWithoutTouchingTheBalance(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST)
	w := testkit.New(t, now)
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "t1", Input: task.Input{Title: "レポート", Minutes: 30, Deadline: now.Add(3 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	key, _ := w.Shortcut.Issue(ctx, uid)
	if n, err := w.Shortcut.Close(ctx, key); err != nil || n.Message != "おつかれ。次は「レポート」の一手、いってみよっか。" {
		t.Fatalf("close without open %+v %v", n, err)
	}
	if _, err := w.Shortcut.Open(ctx, key); err != nil {
		t.Fatal(err)
	}
	before, _, _ := w.Wallet.Get(ctx, uid)
	w.Clock.Advance(12 * time.Minute)
	n, err := w.Shortcut.Close(ctx, key)
	if err != nil || !strings.Contains(n.Title, "DOPAGAKI") || n.Message != "YouTube、12分だったね。次は「レポート」の一手、いってみよっか。" {
		t.Fatalf("close %+v %v", n, err)
	}
	after, _, _ := w.Wallet.Get(ctx, uid)
	if after.Balance != before.Balance || after.Today.Viewed != before.Today.Viewed {
		t.Fatalf("balance changed %+v -> %+v", before, after)
	}
	// 同じ「開いた」に2回目の「閉じた」が来ても数えない。
	w.Clock.Advance(time.Minute)
	if n, err := w.Shortcut.Close(ctx, key); err != nil || !strings.HasPrefix(n.Message, "おつかれ。") {
		t.Fatalf("duplicate close %+v %v", n, err)
	}
	// 閉じたのを取りこぼして3時間を超えたら数えない。
	if _, err := w.Shortcut.Open(ctx, key); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(4 * time.Hour)
	if n, err := w.Shortcut.Close(ctx, key); err != nil || !strings.HasPrefix(n.Message, "おつかれ。") {
		t.Fatalf("missed close %+v %v", n, err)
	}
	err = w.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		today, week, err := w.Shortcut.AppUseMinutes(ctx, tx, uid, w.Clock.Now())
		if today != 12 || week != 12 {
			t.Fatalf("minutes %d %d", today, week)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Shortcut.Close(ctx, "bad"); !errors.Is(err, apperr.Unauthenticated("shortcut.key_malformed")) {
		t.Fatalf("malformed = %v", err)
	}
}
