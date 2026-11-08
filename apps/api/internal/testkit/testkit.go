// Package testkit は、サービスのテストで使う組み立て済みの依存（メモリのストア・進められる時計）。テストからだけ使う。
// エミュレーターの Firestore など別の保存先で組み立てるときは NewWithBackend を使う。
package testkit

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/idempotency"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/records"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/shortcut"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/worksession"
)

// JST は日本時間。
var JST = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		panic(err)
	}
	return loc
}()

// World は組み立てたすべての機能。
type World struct {
	Clock *clock.Fake
	IDs   *id.Sequence
	// Store はメモリの保存先（NewWithBackend で別の保存先を使ったときは nil）。
	Store *memory.Store
	// Backend は実際に使っている保存先。
	Backend     store.Backend
	Runner      *store.Runner
	Idempotency *idempotency.Service
	Planner     *FakePlanner
	Profiles    *profile.Service
	Nights      *sleep.Nights
	Wallet      *wallet.Service
	Sleep       *sleep.Service
	Tasks       *task.Service
	Work        *worksession.Service
	Viewing     *viewing.Service
	Records     *records.Service
	Shortcut    *shortcut.Service
	Log         *slog.Logger
}

// FakePlanner は AI の代わり。Err が nil なら Result を返す。
type FakePlanner struct {
	Result task.DetailResult
	Err    error
	Calls  atomic.Int64
}

func (p *FakePlanner) Detail(context.Context, string, task.DetailRequest) (task.DetailResult, error) {
	p.Calls.Add(1)
	if p.Err != nil {
		return task.DetailResult{}, p.Err
	}
	return p.Result, nil
}

func (p *FakePlanner) Extract(context.Context, string, string, time.Time, *time.Location) ([]task.Input, error) {
	return nil, p.Err
}

func (p *FakePlanner) Adjust(context.Context, string, task.AdjustRequest) ([]task.ChangeInput, error) {
	return nil, p.Err
}

type lines struct{}

func (lines) AppOpenLine(context.Context, string, string) (string, error)     { return "", nil }
func (lines) BalanceZeroLine(context.Context, string, string) (string, error) { return "", nil }

// New は now の時点の、メモリの保存先で組み立てた World を作る。AI は使えない（逆算の計画になる）。
func New(t testing.TB, now time.Time) *World {
	t.Helper()
	mem := memory.NewStore()
	w := NewWithBackend(t, now, mem)
	w.Store = mem
	return w
}

// NewWithBackend は backend を保存先にして World を作る。
func NewWithBackend(t testing.TB, now time.Time, backend store.Backend) *World {
	t.Helper()
	w := &World{Clock: clock.NewFake(now), IDs: &id.Sequence{Prefix: "id"}, Backend: backend}
	w.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	w.Runner = store.NewRunner(backend)
	w.Idempotency = idempotency.New(w.Runner, w.Clock, w.IDs)
	w.Planner = &FakePlanner{Err: unavailable()}
	hooks := calendar.Disabled{}
	w.Profiles = profile.New(w.Runner, w.Clock, hooks)
	w.Nights = sleep.NewNights(w.Profiles)
	sessions := worksession.Sessions{}
	w.Wallet = wallet.New(w.Runner, w.Clock, w.Profiles, w.Nights, sessions)
	w.Sleep = sleep.New(w.Runner, w.Clock, w.Profiles, w.Wallet, hooks)
	w.Tasks = task.New(w.Runner, w.Clock, w.IDs, task.Deps{Planner: w.Planner, Calendar: hooks, Sessions: sessions, Profiles: w.Profiles, Rewards: w.Wallet})
	w.Work = worksession.New(w.Runner, w.Clock, w.IDs, w.Tasks, w.Wallet)
	w.Viewing = viewing.New(w.Runner, w.Clock, w.Wallet, w.Nights)
	w.Records = records.New(w.Clock, w.Profiles, w.Tasks, w.Wallet, w.Viewing)
	w.Shortcut = shortcut.New(w.Runner, w.Clock, w.Tasks, w.Wallet, w.Profiles, lines{}, "http://app.test", w.Log)
	return w
}

// CreateTask は課題を登録する。
func (w *World) CreateTask(t testing.TB, uid, title string, minutes int, deadline time.Time) task.Task {
	t.Helper()
	c, err := w.Tasks.Create(context.Background(), uid, task.CreateRequest{Input: task.Input{Title: title, Minutes: minutes, Deadline: deadline}})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return c.Task
}

// Account は口座を返す。
func (w *World) Account(t testing.TB, uid string) wallet.Account {
	t.Helper()
	a, _, err := w.Wallet.Get(context.Background(), uid)
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	return a
}

// Entries は台帳のすべての項目。
func (w *World) Entries(t testing.TB, uid string) []wallet.Entry {
	t.Helper()
	var out []wallet.Entry
	err := w.Runner.View(context.Background(), func(ctx context.Context, tx *store.Tx) error {
		var err error
		out, err = w.Wallet.Entries(ctx, tx, uid)
		return err
	})
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	return out
}

// CheckProjection は、保存している口座が台帳から作り直した結果と一致するかを確かめる。
func (w *World) CheckProjection(t testing.TB, uid string) {
	t.Helper()
	var a wallet.Account
	var found bool
	var entries []wallet.Entry
	err := w.Runner.View(context.Background(), func(ctx context.Context, tx *store.Tx) error {
		var err error
		if a, found, err = store.Get[wallet.Account](ctx, tx, store.UserDoc(uid, "wallet")); err != nil {
			return err
		}
		entries, err = w.Wallet.Entries(ctx, tx, uid)
		return err
	})
	if err != nil || !found {
		t.Fatalf("projection: found=%v err=%v", found, err)
	}
	p := wallet.Rebuild(entries)
	today := p.Days[a.Day]
	if a.Balance != p.Balance || a.Deferred != p.Deferred || a.LastSeq != p.LastSeq ||
		a.Today.Viewed != today.Viewed || a.Today.Reward != today.Reward || a.Today.Work != today.Work {
		t.Fatalf("projection mismatch: account=%+v rebuild=%+v today=%+v", a, p, today)
	}
}

func unavailable() error { return apperr.Unavailable("ai.unavailable") }
