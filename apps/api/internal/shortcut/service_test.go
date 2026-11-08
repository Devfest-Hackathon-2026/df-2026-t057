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
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/shortcut"
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
	o, err := w.Shortcut.Open(ctx, key)
	if err != nil || o.OpenID == "" || o.Wait < shortcut.FirstWaitMin || o.Wait > shortcut.MaxWait {
		t.Fatalf("%+v %v", o, err)
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

// world は、締切が遠い課題を1つ登録し、接続キーを発行した World。
func world(t *testing.T, deadline time.Duration) (*testkit.World, string) {
	t.Helper()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST)
	w := testkit.New(t, now)
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "t1", Input: task.Input{Title: "レポート", Minutes: 30, Deadline: now.Add(deadline)}}); err != nil {
		t.Fatal(err)
	}
	key, err := w.Shortcut.Issue(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return w, key
}

func available(t *testing.T, w *testkit.World) time.Duration {
	t.Helper()
	a, _, err := w.Wallet.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return time.Duration(a.Available()) * time.Second
}

func check(t *testing.T, w *testkit.World, key, openID string) shortcut.Checked {
	t.Helper()
	c, err := w.Shortcut.Check(ctx, key, openID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCheckNotifiesOnceWhenTimeRunsOut(t *testing.T) {
	w, key := world(t, 6*time.Hour)
	left := available(t, w)
	o, err := w.Shortcut.Open(ctx, key)
	if err != nil || o.Wait != left {
		t.Fatalf("open %+v %v (left %v)", o, err, left)
	}
	w.Clock.Advance(left - time.Minute)
	if c := check(t, w, key, o.OpenID); c.Action != shortcut.ActionWait || c.Wait != time.Minute {
		t.Fatalf("early %+v", c)
	}
	w.Clock.Advance(time.Minute)
	c := check(t, w, key, o.OpenID)
	if c.Action != shortcut.ActionNotify || !c.Notification.OpenApp || c.Notification.URL != "http://app.test/?notification=t1" ||
		!strings.Contains(c.Notification.Title, "DOPAGAKI") || !strings.Contains(c.Notification.Message, "区切ろう") {
		t.Fatalf("due %+v", c)
	}
	if c := check(t, w, key, o.OpenID); c.Action != shortcut.ActionNone {
		t.Fatalf("second notification %+v", c)
	}
}

func TestCheckWaitsAgainWhenTimeWasEarnedMeanwhile(t *testing.T) {
	w, key := world(t, 6*time.Hour)
	left := available(t, w)
	o, _ := w.Shortcut.Open(ctx, key)
	// 待っている間に課題をもう1つ登録して、持ち時間が増えた。
	if _, err := w.Tasks.Create(ctx, uid, task.CreateRequest{ID: "t2", Input: task.Input{Title: "演習", Minutes: 30, Deadline: w.Clock.Now().Add(8 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	earned := available(t, w) - left
	if earned <= 0 {
		t.Fatalf("earned %v", earned)
	}
	w.Clock.Advance(left)
	if c := check(t, w, key, o.OpenID); c.Action != shortcut.ActionWait || c.Wait != earned {
		t.Fatalf("after earning %+v (earned %v)", c, earned)
	}
}

func TestCheckStaysSilentAfterCloseOrReopen(t *testing.T) {
	w, key := world(t, 6*time.Hour)
	left := available(t, w)
	first, _ := w.Shortcut.Open(ctx, key)
	w.Clock.Advance(time.Minute)
	if err := w.Shortcut.Close(ctx, key); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(left)
	if c := check(t, w, key, first.OpenID); c.Action != shortcut.ActionNone {
		t.Fatalf("closed %+v", c)
	}
	older, _ := w.Shortcut.Open(ctx, key)
	w.Clock.Advance(time.Second)
	newer, _ := w.Shortcut.Open(ctx, key)
	w.Clock.Advance(left)
	if c := check(t, w, key, older.OpenID); c.Action != shortcut.ActionNone {
		t.Fatalf("reopened %+v", c)
	}
	if c := check(t, w, key, newer.OpenID); c.Action != shortcut.ActionNotify {
		t.Fatalf("newer %+v", c)
	}
	if _, err := w.Shortcut.Check(ctx, key, "x"); !errors.Is(err, apperr.Invalid("request.invalid")) {
		t.Fatalf("bad id = %v", err)
	}
}

func TestTaskStartNotifiesWithoutOpeningTheApp(t *testing.T) {
	// 締切40分前・30分の課題は、10分後に始める計画になる。
	w, key := world(t, 40*time.Minute)
	_, rev, err := w.Wallet.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeSelf, 1800, rev); err != nil {
		t.Fatal(err)
	}
	o, _ := w.Shortcut.Open(ctx, key)
	if o.Wait != 10*time.Minute {
		t.Fatalf("open %+v", o)
	}
	w.Clock.Advance(10 * time.Minute)
	c := check(t, w, key, o.OpenID)
	if c.Action != shortcut.ActionNotify || c.Notification.OpenApp || !strings.Contains(c.Notification.Message, "レポート") {
		t.Fatalf("task %+v", c)
	}
}

func TestOpenDoesNotWaitWithNothingToWatch(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	key, _ := w.Shortcut.Issue(ctx, uid)
	_, rev, err := w.Wallet.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeSelf, 1800, rev); err != nil {
		t.Fatal(err)
	}
	if o, err := w.Shortcut.Open(ctx, key); err != nil || o.Wait != 0 {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestChecksAreCountedApartFromOpens(t *testing.T) {
	w, key := world(t, 6*time.Hour)
	var last shortcut.Opened
	for i := range shortcut.PerAccountPerMinute {
		var err error
		if last, err = w.Shortcut.Open(ctx, key); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		w.Clock.Advance(time.Millisecond)
	}
	if err := w.Shortcut.Close(ctx, key); !errors.Is(err, apperr.RateLimited("shortcut.rate_limited", 0)) {
		t.Fatalf("close = %v", err)
	}
	if _, err := w.Shortcut.Check(ctx, key, last.OpenID); err != nil {
		t.Fatalf("check = %v", err)
	}
}

func TestCloseRecordsUseWithoutTouchingTheBalance(t *testing.T) {
	w, key := world(t, 6*time.Hour)
	if err := w.Shortcut.Close(ctx, key); err != nil {
		t.Fatalf("close without open %v", err)
	}
	if _, err := w.Shortcut.Open(ctx, key); err != nil {
		t.Fatal(err)
	}
	before, _, _ := w.Wallet.Get(ctx, uid)
	w.Clock.Advance(12 * time.Minute)
	if err := w.Shortcut.Close(ctx, key); err != nil {
		t.Fatal(err)
	}
	after, _, _ := w.Wallet.Get(ctx, uid)
	if after.Balance != before.Balance || after.Today.Viewed != before.Today.Viewed {
		t.Fatalf("balance changed %+v -> %+v", before, after)
	}
	// 一瞬だけ通った回は、回数にも時間にも数えない。
	w.Clock.Advance(time.Minute)
	if _, err := w.Shortcut.Open(ctx, key); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(10 * time.Second)
	if err := w.Shortcut.Close(ctx, key); err != nil {
		t.Fatal(err)
	}
	err := w.Runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		opens, _, _, err := w.Shortcut.AppOpenCounts(ctx, tx, uid, w.Clock.Now())
		if err != nil {
			return err
		}
		today, week, err := w.Shortcut.AppUseMinutes(ctx, tx, uid, w.Clock.Now())
		if opens != 1 || today != 12 || week != 12 {
			t.Fatalf("opens %d minutes %d %d", opens, today, week)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Shortcut.Close(ctx, "bad"); !errors.Is(err, apperr.Unauthenticated("shortcut.key_malformed")) {
		t.Fatalf("malformed = %v", err)
	}
}
