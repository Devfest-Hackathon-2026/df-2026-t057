package sleep_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
)

const uid = "u1"

var ctx = context.Background()

func TestNightRewardsAreDeferredAndReleasedInTheMorning(t *testing.T) {
	jst := testkit.JST
	w := testkit.New(t, time.Date(2026, 11, 7, 12, 0, 0, 0, jst))
	// 残高を1200にしておく（600 + 登録2件）。
	w.CreateTask(t, uid, "a", 30, time.Date(2026, 11, 20, 0, 0, 0, 0, jst))
	w.CreateTask(t, uid, "b", 30, time.Date(2026, 11, 20, 0, 0, 0, 0, jst))
	if _, err := w.Sleep.Update(ctx, uid, sleep.Schedule{Bedtime: "23:30", Wake: "07:00"}, 0); err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(time.Date(2026, 11, 8, 23, 0, 0, 0, jst)) // 夜の時間帯
	w.CreateTask(t, uid, "night", 30, time.Date(2026, 11, 20, 0, 0, 0, 0, jst))
	a := w.Account(t, uid)
	if a.Balance != 1200 || a.Deferred != 300 {
		t.Fatalf("balance=%d deferred=%d", a.Balance, a.Deferred)
	}
	if m, err := w.Sleep.SettleMorning(ctx, uid); err != nil || m != nil {
		t.Fatalf("no settlement before wake: %+v %v", m, err)
	}
	w.Clock.Set(time.Date(2026, 11, 9, 7, 30, 0, 0, jst))
	m, err := w.Sleep.SettleMorning(ctx, uid)
	if err != nil || m == nil || !m.Kept || m.ReleasedSeconds != 300 || m.BonusSeconds != 300 {
		t.Fatalf("morning %+v %v", m, err)
	}
	if a := w.Account(t, uid); a.Balance != 1800 || a.Deferred != 0 {
		t.Fatalf("after morning %+v", a)
	}
	if m, _ := w.Sleep.SettleMorning(ctx, uid); m != nil {
		t.Fatal("second settlement")
	}
	st, _ := w.Sleep.Get(ctx, uid)
	if st.Sleep.KeptNights != 1 {
		t.Fatalf("kept = %d", st.Sleep.KeptNights)
	}
	w.CheckProjection(t, uid)
}

func TestMorningWithNothingToSettleDoesNotWrite(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 9, 9, 0, 0, 0, testkit.JST))
	st, err := w.Sleep.Update(ctx, uid, sleep.Schedule{Bedtime: "23:30", Wake: "07:00"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	m, err := w.Sleep.SettleMorning(ctx, uid)
	if err != nil || m != nil {
		t.Fatalf("%+v %v", m, err)
	}
	after, _ := w.Sleep.Get(ctx, uid)
	if after.Revision != st.Revision {
		t.Fatalf("revision changed %d -> %d", st.Revision, after.Revision)
	}
}

func TestClearReleasesHeldRewardsAndRequiresIfMatch(t *testing.T) {
	jst := testkit.JST
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, jst))
	st, err := w.Sleep.Update(ctx, uid, sleep.Schedule{Bedtime: "23:30", Wake: "07:00"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Sleep.Update(ctx, uid, sleep.Schedule{Bedtime: "23:00", Wake: "07:00"}, st.Revision-1); !errors.Is(err, apperr.PreconditionFailed("precondition.failed")) {
		t.Fatalf("stale = %v", err)
	}
	w.Clock.Set(time.Date(2026, 11, 8, 23, 0, 0, 0, jst))
	w.CreateTask(t, uid, "n", 30, time.Date(2026, 11, 20, 0, 0, 0, 0, jst))
	cleared, err := w.Sleep.Clear(ctx, uid, st.Revision)
	if err != nil || cleared.Sleep != nil {
		t.Fatalf("clear %+v %v", cleared, err)
	}
	if a := w.Account(t, uid); a.Balance != 900 || a.Deferred != 0 {
		t.Fatalf("after clear %+v", a)
	}
	w.CheckProjection(t, uid)
}

func TestShortsAtNightBreaksTheStreak(t *testing.T) {
	jst := testkit.JST
	w := testkit.New(t, time.Date(2026, 11, 7, 12, 0, 0, 0, jst))
	if _, err := w.Sleep.Update(ctx, uid, sleep.Schedule{Bedtime: "23:30", Wake: "07:00"}, 0); err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(time.Date(2026, 11, 8, 23, 0, 0, 0, jst))
	v, err := w.Viewing.Start(ctx, uid, "view-0001", 300, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(20 * time.Second)
	if _, err := w.Viewing.Report(ctx, uid, "rep-00001", 10, v.Session.ID); err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(time.Date(2026, 11, 9, 8, 0, 0, 0, jst))
	m, err := w.Sleep.SettleMorning(ctx, uid)
	if err != nil || m == nil || m.Kept || m.BonusSeconds != 0 {
		t.Fatalf("morning %+v %v", m, err)
	}
}
