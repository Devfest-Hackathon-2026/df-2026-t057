package viewing_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/testkit"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

func TestBacklogRangesAreChargedWithoutWaitingAgain(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(fmt.Sprint("free=", free), func(t *testing.T) {
			w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
			sessionID := ""
			if free {
				_, rev, _ := w.Wallet.Get(ctx, uid)
				if _, _, err := w.Wallet.UpdatePolicy(ctx, uid, wallet.ModeSelf, 1800, rev); err != nil {
					t.Fatal(err)
				}
			} else {
				v, err := w.Viewing.Start(ctx, uid, "range-session", 300, "")
				if err != nil {
					t.Fatal(err)
				}
				sessionID = v.Session.ID
			}
			// 未送信分を最初の10秒と次の54秒に分けて、同じ時刻に送る。
			if free {
				if _, err := w.Viewing.ReportFrom(ctx, uid, "range-initial", 1, sessionID, 0); err != nil {
					t.Fatal(err)
				}
			}
			w.Clock.Advance(100 * time.Second)
			base := 0
			if free {
				base = 1
			}
			if _, err := w.Viewing.ReportFrom(ctx, uid, "range-first", 10, sessionID, base); err != nil {
				t.Fatal(err)
			}
			v, err := w.Viewing.ReportFrom(ctx, uid, "range-next", 54, sessionID, base+10)
			if err != nil || v.ReportCursor != base+64 || v.AcceptedSeconds == nil || *v.AcceptedSeconds != 54 {
				t.Fatalf("backlog: %+v %v", v, err)
			}
			// 別IDでも同じ計測区間なら重複して消費しない。
			if _, err := w.Viewing.ReportFrom(ctx, uid, "range-overlap", 54, sessionID, base+10); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Viewing.ReportFrom(ctx, uid, "range-next", 54, sessionID, base+10); err != nil {
				t.Fatal(err)
			}
			a := w.Account(t, uid)
			if a.Today.Viewed != base+64 {
				t.Fatalf("viewed=%d", a.Today.Viewed)
			}
			w.CheckProjection(t, uid)
		})
	}
}

func TestRangeReportRetainsUnacceptedRemainderAndClockBound(t *testing.T) {
	w := testkit.New(t, time.Date(2026, 11, 8, 12, 0, 0, 0, testkit.JST))
	if _, err := w.Viewing.Start(ctx, uid, "range-session", 300, ""); err != nil {
		t.Fatal(err)
	}
	w.Clock.Advance(3 * time.Second)
	v, err := w.Viewing.ReportFrom(ctx, uid, "partial-first", 10, "range-session", 0)
	if err != nil || v.ReportCursor != 3 || v.AcceptedSeconds == nil || *v.AcceptedSeconds != 3 {
		t.Fatalf("partial: %+v %v", v, err)
	}
	w.Clock.Advance(7 * time.Second)
	v, err = w.Viewing.ReportFrom(ctx, uid, "partial-next", 7, "range-session", 3)
	if err != nil || v.ReportCursor != 10 || *v.AcceptedSeconds != 7 {
		t.Fatalf("remainder: %+v %v", v, err)
	}
	if _, err := w.Viewing.ReportFrom(ctx, uid, "future-range", 1, "range-session", 20); err == nil {
		t.Fatal("accepted a gap")
	}
	w.CheckProjection(t, uid)
}
