package wallet_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

const uid = "u1"

var ctx = context.Background()

// noon は 2026-11-08 12:00 JST。
var noon = time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST)

func view(t *testing.T, w *testkit.World, receipt string, seconds int) {
	t.Helper()
	err := w.Runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		_, err := w.Wallet.RecordViewing(ctx, tx, uid, receipt, seconds, w.Clock.Now())
		if err != nil {
			return err
		}
		_, err = tx.Touch(ctx, uid, store.ResourceViewing)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fill は登録の報酬で残高を上限（1800）にする。
func fill(t *testing.T, w *testkit.World) {
	t.Helper()
	for i := 0; w.Account(t, uid).Balance < wallet.BalanceCapSeconds; i++ {
		w.CreateTask(t, uid, fmt.Sprintf("課題%d", i), 30, w.Clock.Now().Add(240*time.Hour))
	}
}

// workTask は作業に使う課題を登録する（登録の報酬が付く）。
func workTask(t *testing.T, w *testkit.World) string {
	t.Helper()
	return w.CreateTask(t, uid, "作業", 720, w.Clock.Now().Add(240*time.Hour)).ID
}

// work は課題の作業をしてから記録する（報酬の計算を確かめる）。
func work(t *testing.T, w *testkit.World, taskID string, seconds int) wallet.WorkResult {
	t.Helper()
	s, err := w.Work.Start(ctx, uid, taskID)
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(time.Duration(seconds) * time.Second)
	r, err := w.Work.Confirm(ctx, uid, s.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewAccountStartsWithInitialEntry(t *testing.T) {
	w := testkit.New(t, noon)
	a := w.Account(t, uid)
	entries := w.Entries(t, uid)
	if a.Balance != 600 || len(entries) != 1 || entries[0].Kind != wallet.KindInitial {
		t.Fatalf("balance=%d entries=%+v", a.Balance, entries)
	}
	w.CheckProjection(t, uid)
}

func TestMidnightRefillHappensOnceDespiteConcurrentReads(t *testing.T) {
	w := testkit.New(t, noon)
	view(t, w, "r1-abcdefgh", 360)
	if got := w.Account(t, uid).Balance; got != 240 {
		t.Fatalf("balance = %d", got)
	}
	w.Clock.Set(time.Date(2026, 11, 9, 0, 0, 1, 0, testkit.JST))
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = w.Wallet.Get(ctx, uid)
		}()
	}
	wg.Wait()
	a := w.Account(t, uid)
	dailies := 0
	for _, e := range w.Entries(t, uid) {
		if e.Kind == wallet.KindDaily {
			dailies++
			if e.Seconds != 360 {
				t.Fatalf("daily = %d", e.Seconds)
			}
		}
	}
	if a.Balance != 600 || dailies != 1 || a.Day != "2026-11-09" || a.Today.Viewed != 0 {
		t.Fatalf("balance=%d dailies=%d day=%s", a.Balance, dailies, a.Day)
	}
	if len(a.History) != 1 || a.History[0].Viewed != 360 {
		t.Fatalf("history = %+v", a.History)
	}
	w.CheckProjection(t, uid)
}

func TestMissedDaysNeitherAddNorRemove(t *testing.T) {
	w := testkit.New(t, noon)
	w.CreateTask(t, uid, "a", 30, noon.Add(240*time.Hour))
	if got := w.Account(t, uid).Balance; got != 900 {
		t.Fatalf("balance = %d", got)
	}
	w.Clock.Advance(4 * 24 * time.Hour)
	a := w.Account(t, uid)
	if a.Balance != 900 {
		t.Fatalf("balance after 4 days = %d", a.Balance)
	}
	w.CheckProjection(t, uid)
}

func TestRolloverFollowsProfileTimeZone(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)) // 11/9 05:00 JST
	if a := w.Account(t, uid); a.Day != "2026-11-09" {
		t.Fatalf("day = %s", a.Day)
	}
	if _, _, err := w.Profiles.Update(ctx, uid, profile.Input{TimeZone: "UTC", Locale: locale.Ja}, 0); err != nil {
		t.Fatal(err)
	}
	// UTC では 11/8 のまま。日付は戻さない。
	if a := w.Account(t, uid); a.Day != "2026-11-09" {
		t.Fatalf("day after zone change = %s", a.Day)
	}
	w.Clock.Set(time.Date(2026, 11, 10, 0, 0, 1, 0, time.UTC))
	a := w.Account(t, uid)
	if a.Day != "2026-11-10" || !a.ResetAt.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day=%s resetAt=%s", a.Day, a.ResetAt)
	}
}

func TestWorkCountsOnlyRunningIntervals(t *testing.T) {
	w := testkit.New(t, noon)
	task := w.CreateTask(t, uid, "レポート", 60, noon.Add(48*time.Hour))
	s, err := w.Work.Start(ctx, uid, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(3 * time.Minute)
	if _, err := w.Work.Pause(ctx, uid); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(40 * time.Minute)
	if _, err := w.Work.Resume(ctx, uid); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(2 * time.Minute)
	var r wallet.WorkResult
	for range 3 {
		if r, err = w.Work.Confirm(ctx, uid, s.Session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if r.WorkSeconds != 300 || r.RewardSeconds != 60 {
		t.Fatalf("result = %+v", r)
	}
	a := w.Account(t, uid)
	if a.Balance != 960 {
		t.Fatalf("balance = %d", a.Balance)
	}
	got, _, err := w.Tasks.Get(ctx, uid, task.ID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("task status = %s err=%v", got.Status, err)
	}
	// 4分の作業は5分に満たないので切り上げない。
	r = work(t, w, task.ID, 240)
	if r.RewardSeconds != 0 || w.Account(t, uid).Today.Work != 540 {
		t.Fatalf("follow-up = %+v work=%d", r, w.Account(t, uid).Today.Work)
	}
	w.CheckProjection(t, uid)
}

func TestSmallBlocksCombine(t *testing.T) {
	w := testkit.New(t, noon)
	id := workTask(t, w)
	if r := work(t, w, id, 120); r.RewardSeconds != 0 {
		t.Fatalf("first = %+v", r)
	}
	if r := work(t, w, id, 180); r.RewardSeconds != 60 {
		t.Fatalf("second = %+v", r)
	}
	w.CheckProjection(t, uid)
}

func TestBalanceCapGrantsActualIncrementAndNeverPaysLater(t *testing.T) {
	w := testkit.New(t, noon)
	id := workTask(t, w)
	fill(t, w)
	view(t, w, "r-cap-0001", 10)
	if got := w.Account(t, uid).Balance; got != 1790 {
		t.Fatalf("balance = %d", got)
	}
	// 登録の報酬は上限で10秒になり、後から残りを払わない。
	w.CreateTask(t, uid, "cap", 30, noon.Add(240*time.Hour))
	a := w.Account(t, uid)
	if a.Balance != 1800 {
		t.Fatalf("balance after capped registration = %d", a.Balance)
	}
	r := work(t, w, id, 300)
	// 上限に達しているので作業の報酬は0（後から払わない）。
	if r.RewardSeconds != 0 {
		t.Fatalf("reward = %+v", r)
	}
	view(t, w, "r-cap-0003", 300)
	r = work(t, w, id, 300)
	if r.RewardSeconds != 60 || w.Account(t, uid).Today.Reward != 60 {
		t.Fatalf("reward = %+v today=%+v", r, w.Account(t, uid).Today)
	}
	w.CheckProjection(t, uid)
}

func TestWorkRewardRespectsCapAndCappedPartIsLost(t *testing.T) {
	w := testkit.New(t, noon)
	id := workTask(t, w)
	fill(t, w)
	if r := work(t, w, id, 300); r.RewardSeconds != 0 {
		t.Fatalf("at cap = %+v", r)
	}
	view(t, w, "r-c-000001", 10)
	r := work(t, w, id, 300)
	if r.RewardSeconds != 10 {
		t.Fatalf("reward = %+v", r)
	}
	a := w.Account(t, uid)
	if a.Today.Reward != 10 || a.Balance != 1800 {
		t.Fatalf("today=%+v balance=%d", a.Today, a.Balance)
	}
	w.CheckProjection(t, uid)
}

func TestDailyRewardCapHoldsAfterSpending(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 1, 0, 0, 0, testkit.JST))
	id := workTask(t, w)
	total := 0
	for i := range 8 {
		view(t, w, fmt.Sprintf("spend-%04d", i), 1800)
		total += work(t, w, id, 1500).RewardSeconds
	}
	a := w.Account(t, uid)
	if a.Today.Reward != wallet.DailyRewardCapSeconds || total != wallet.DailyRewardCapSeconds {
		t.Fatalf("reward today=%d total=%d", a.Today.Reward, total)
	}
	w.CheckProjection(t, uid)
}

func TestAbandonedTimerIsCappedAtLimit(t *testing.T) {
	w := testkit.New(t, noon)
	r := work(t, w, workTask(t, w), 5*3600)
	if r.WorkSeconds != 1500 {
		t.Fatalf("work = %+v", r)
	}
}

func TestWorkAcrossMidnightCreditsOnlyToday(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 14, 50, 0, 0, time.UTC))
	task := w.CreateTask(t, uid, "夜", 60, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	s, err := w.Work.Start(ctx, uid, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(time.Date(2026, 11, 8, 15, 15, 0, 0, time.UTC))
	r, err := w.Work.Confirm(ctx, uid, s.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.WorkSeconds != 900 || r.RewardSeconds != 180 {
		t.Fatalf("result = %+v", r)
	}
	w.CheckProjection(t, uid)
}

func TestConfirmByAnotherAccountConflicts(t *testing.T) {
	w := testkit.New(t, noon)
	task := w.CreateTask(t, uid, "a", 30, noon.Add(48*time.Hour))
	s, err := w.Work.Start(ctx, uid, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Work.Confirm(ctx, "u2", s.Session.ID)
	if !errors.Is(err, apperr.Conflict("work.no_session")) {
		t.Fatalf("err = %v", err)
	}
}

func TestParallelConfirmationsGrantOnce(t *testing.T) {
	w := testkit.New(t, noon)
	task := w.CreateTask(t, uid, "a", 30, noon.Add(48*time.Hour))
	s, err := w.Work.Start(ctx, uid, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(5 * time.Minute)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Work.Confirm(ctx, uid, s.Session.ID)
		}()
	}
	wg.Wait()
	if a := w.Account(t, uid); a.Balance != 960 {
		t.Fatalf("balance = %d", a.Balance)
	}
	w.CheckProjection(t, uid)
}

func TestPolicyChangesKeepBalanceAndFixedRefillsToLimit(t *testing.T) {
	w := testkit.New(t, noon)
	_, rev, err := w.Wallet.Get(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []wallet.Mode{wallet.ModeFixed, wallet.ModeReminder, wallet.ModeSelf, wallet.ModeFixed} {
		var a wallet.Account
		if a, rev, err = w.Wallet.UpdatePolicy(ctx, uid, m, 1200, rev); err != nil {
			t.Fatal(err)
		}
		if a.Balance != 600 {
			t.Fatalf("%s balance = %d", m, a.Balance)
		}
	}
	if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeFixed, 1200, rev-1); !errors.Is(err, apperr.PreconditionFailed("precondition.failed")) {
		t.Fatalf("stale If-Match = %v", err)
	}
	w.Clock.Advance(24 * time.Hour)
	if a := w.Account(t, uid); a.Balance != 1200 {
		t.Fatalf("fixed refill balance = %d", a.Balance)
	}
	w.CheckProjection(t, uid)
}

func TestPolicyLockedWhileWorking(t *testing.T) {
	w := testkit.New(t, noon)
	task := w.CreateTask(t, uid, "a", 30, noon.Add(48*time.Hour))
	if _, err := w.Work.Start(ctx, uid, task.ID); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := w.Wallet.Get(ctx, uid)
	if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeFixed, 1200, rev); !errors.Is(err, apperr.Conflict("work.active")) {
		t.Fatalf("err = %v", err)
	}
}

func TestLedgerPagination(t *testing.T) {
	w := testkit.New(t, noon)
	for i := range 5 {
		w.CreateTask(t, uid, fmt.Sprint(i), 10, noon.Add(48*time.Hour))
	}
	page, next, err := w.Wallet.Ledger(ctx, uid, 4, "")
	if err != nil || len(page) != 4 || next == "" || page[0].Seq != 6 {
		t.Fatalf("page=%d next=%q err=%v", len(page), next, err)
	}
	rest, next2, err := w.Wallet.Ledger(ctx, uid, 4, next)
	if err != nil || len(rest) != 2 || next2 != "" || rest[1].Kind != wallet.KindInitial {
		t.Fatalf("rest=%+v next=%q err=%v", rest, next2, err)
	}
}

func TestRepairRebuildsFromLedger(t *testing.T) {
	w := testkit.New(t, noon)
	w.CreateTask(t, uid, "a", 10, noon.Add(48*time.Hour))
	err := w.Runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		a, _, err := store.Get[wallet.Account](ctx, tx, store.UserDoc(uid, "wallet"))
		if err != nil {
			return err
		}
		a.Balance = 1
		return tx.Set(store.UserDoc(uid, "wallet"), a)
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.Wallet.Repair(ctx, uid)
	if err != nil || a.Balance != 900 {
		t.Fatalf("repair = %d err=%v", a.Balance, err)
	}
	w.CheckProjection(t, uid)
}
