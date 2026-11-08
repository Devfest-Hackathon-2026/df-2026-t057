package viewing_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

const uid = "u1"

var ctx = context.Background()

func TestReportRequiresSession(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	if _, err := w.Viewing.Report(ctx, uid, "rep-00001", 10, ""); !errors.Is(err, apperr.Conflict("viewing.session_required")) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionAlreadyActiveAndExtend(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	v, err := w.Viewing.Start(ctx, uid, "view-0001", 300, "")
	if err != nil || v.Session.PlannedSeconds != 300 {
		t.Fatalf("%+v %v", v, err)
	}
	if again, err := w.Viewing.Start(ctx, uid, "view-0001", 300, ""); err != nil || again.Session.ID != "view-0001" {
		t.Fatalf("idempotent start %v", err)
	}
	if _, err := w.Viewing.Start(ctx, uid, "view-0002", 300, ""); !errors.Is(err, apperr.Conflict("viewing.session_active")) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.Viewing.End(ctx, uid, "view-0001", viewing.EndStop); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Viewing.Start(ctx, uid, "view-0003", 300, "other"); !errors.Is(err, apperr.Conflict("viewing.extend_mismatch")) {
		t.Fatalf("err = %v", err)
	}
	ext, err := w.Viewing.Start(ctx, uid, "view-0004", 300, "view-0001")
	if err != nil || ext.Session.ExtendFrom != "view-0001" {
		t.Fatalf("%+v %v", ext, err)
	}
	if _, err := w.Viewing.End(ctx, uid, "unknown-1", viewing.EndStop); !errors.Is(err, apperr.NotFound("viewing.session_unknown")) {
		t.Fatalf("err = %v", err)
	}
}

func TestExhaustionEndsSessionAndRefillReopens(t *testing.T) {
	jst := testkit.JST
	w := testkit.New(t, time.Date(2026, 11, 8, 20, 0, 0, 0, jst))
	// 今日 1790 秒見て、残高を 8 秒にする。
	v, err := w.Viewing.Start(ctx, uid, "view-0001", 600, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		w.Clock.Advance(time.Minute)
		if v, err = w.Viewing.Report(ctx, uid, fmt.Sprintf("r-%07d", i), 60, v.Session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if v.Balance != 0 || v.Session.EndedAt == nil {
		t.Fatalf("after spending %+v", v)
	}
	if _, err := w.Viewing.Start(ctx, uid, "view-0002", 300, ""); !errors.Is(err, apperr.Conflict("viewing.exhausted")) {
		t.Fatalf("err = %v", err)
	}
	w.Clock.Set(time.Date(2026, 11, 9, 9, 0, 0, 0, jst))
	v, err = w.Viewing.Start(ctx, uid, "view-0003", 300, "")
	if err != nil || v.Balance != 600 {
		t.Fatalf("%+v %v", v, err)
	}
	w.CheckProjection(t, uid)
}

func TestConcurrentDuplicateReceiptsDebitOnce(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	v, err := w.Viewing.Start(ctx, uid, "view-0001", 300, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(time.Minute)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Viewing.Report(ctx, uid, "same-receipt", 10, v.Session.ID)
		}()
	}
	wg.Wait()
	a := w.Account(t, uid)
	if a.Balance != 590 || a.Today.Viewed != 10 {
		t.Fatalf("%+v", a)
	}
	// 同じ秒の別の受付は、経過時間を超えて減らさない。
	if v, err = w.Viewing.Report(ctx, uid, "next-receipt", 10, v.Session.ID); err != nil || v.Balance != 590 {
		t.Fatalf("%+v %v", v, err)
	}
	w.CheckProjection(t, uid)
}

func TestReportAcrossMidnightIsBookedOnReceiptDay(t *testing.T) {
	jst := testkit.JST
	w := testkit.New(t, time.Date(2026, 11, 7, 23, 59, 50, 0, jst))
	v, err := w.Viewing.Start(ctx, uid, "view-0001", 300, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.Set(time.Date(2026, 11, 8, 0, 0, 5, 0, jst))
	for range 3 {
		if _, err := w.Viewing.Report(ctx, uid, "rep-midnight", 10, v.Session.ID); err != nil {
			t.Fatal(err)
		}
	}
	a := w.Account(t, uid)
	if a.Day != "2026-11-08" || a.Today.Viewed != 10 || a.Balance != 590 {
		t.Fatalf("%+v", a)
	}
	w.CheckProjection(t, uid)
}

func TestSelfModeRecordsWithoutDebit(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	_, rev, _ := w.Wallet.Get(ctx, uid)
	if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeSelf, 1800, rev); err != nil {
		t.Fatal(err)
	}
	v, err := w.Viewing.Report(ctx, uid, "self-00001", 30, "")
	if err != nil || v.Balance != 600 || v.RequiresSession {
		t.Fatalf("%+v %v", v, err)
	}
	if a := w.Account(t, uid); a.Today.Viewed != 30 {
		t.Fatalf("viewed %d", a.Today.Viewed)
	}
	w.CheckProjection(t, uid)
}
