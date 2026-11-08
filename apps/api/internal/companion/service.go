package companion

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// lineTimeout は一言を待つ最長の時間（その場面の表示を遅らせないように）。
const lineTimeout = 6 * time.Second

// maxCachedLines はインスタンスごとに覚えておく一言の数の上限。
const maxCachedLines = 10000

// Deps は Service が使う外部の機能。Generator が nil なら AI を使わない（デモ）。
type Deps struct {
	Tasks     Tasks
	Profiles  Profiles
	Wallet    Wallet
	Sleep     Sleep
	AppOpens  AppOpens
	Generator Generator
}

// Service は相棒の一言のユースケース。
type Service struct {
	runner *store.Runner
	clock  clock.Clock
	Deps
	cache lineCache
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, deps Deps) *Service {
	return &Service{runner: runner, clock: c, Deps: deps, cache: lineCache{entries: map[string]cachedLine{}}}
}

// Line は場面に合わせた一言を返す。taskID を指定したら、その課題が無ければ task.not_found。
// AI が使えない・上限・失敗・不適切な出力のときは Line{Generated: false} を返す（エラーにしない）。
func (s *Service) Line(ctx context.Context, uid string, moment Moment, taskID string) (Line, error) {
	if !moment.Valid() {
		return Line{}, apperr.Invalid("request.invalid", apperr.Field("moment", "not_allowed"))
	}
	now := s.clock.Now()
	var current *task.Task
	var prof profile.Profile
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		if prof, err = s.Profiles.ProfileTx(ctx, tx, uid); err != nil {
			return err
		}
		if taskID == "" {
			return nil
		}
		t, err := s.Tasks.LoadTx(ctx, tx, uid, taskID)
		current = &t
		return err
	})
	if err != nil {
		return Line{}, err
	}
	if s.Generator == nil {
		return Line{}, nil
	}
	loc := profile.LoadLocation(prof.TimeZone)
	key := lineKey(uid, moment, current, now, loc)
	if text, ok := s.cache.get(key, now); ok {
		return Line{Text: text, Generated: true}, nil
	}
	progress, err := s.progress(ctx, uid, now, loc)
	if err != nil {
		return Line{}, err
	}
	req := LineRequest{Moment: moment, Progress: progress, ProfileContext: prof.Context}
	if current != nil {
		summary := Describe(*current, false, loc)
		req.Task = &summary
	}
	lineCtx, cancel := context.WithTimeout(ctx, lineTimeout)
	defer cancel()
	raw, err := s.Generator.Line(lineCtx, uid, req)
	if err != nil {
		// 外部の応答本文や入力を出さないよう、コードだけを残す。
		slog.WarnContext(ctx, "companion line fell back", "moment", string(moment), "code", apperr.As(err).Code)
		return Line{}, nil
	}
	text, ok := CleanLine(raw)
	if !ok {
		return Line{}, nil
	}
	s.cache.put(key, text, now)
	return Line{Text: text, Generated: true}, nil
}

// progress は一言の材料にする進み具合を読む。
func (s *Service) progress(ctx context.Context, uid string, now time.Time, loc *time.Location) (p Progress, err error) {
	err = s.Wallet.Read(ctx, uid, func(ctx context.Context, tx *store.Tx, a *wallet.Account) error {
		tasks, err := s.Tasks.All(ctx, tx, uid)
		if err != nil {
			return err
		}
		st, err := s.Sleep.GetTx(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		opens, err := ReadAppOpens(ctx, tx, s.AppOpens, uid, now)
		if err != nil {
			return err
		}
		p = BuildProgress(ProgressInput{Tasks: tasks, Account: *a, Sleep: SummarizeSleep(st, now, loc), AppOpens: opens, Now: now, Loc: loc})
		return nil
	})
	return p, err
}

// ReadAppOpens はアプリを開いた回数を読む。ショートカットを設定していない（または source が nil の）ときは nil。
func ReadAppOpens(ctx context.Context, tx *store.Tx, source AppOpens, uid string, now time.Time) (*OpenCounts, error) {
	if source == nil {
		return nil, nil
	}
	today, week, configured, err := source.AppOpenCounts(ctx, tx, uid, now)
	if err != nil || !configured {
		return nil, err
	}
	return &OpenCounts{Today: today, Last7Days: week}, nil
}

type cachedLine struct {
	text    string
	expires time.Time
}

// lineCache はインスタンスごとの一言の使い回し。インスタンスをまたいでは共有しない。
type lineCache struct {
	mu      sync.Mutex
	entries map[string]cachedLine
}

func (c *lineCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !now.Before(e.expires) {
		return "", false
	}
	return e.text, true
}

func (c *lineCache) put(key, text string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= maxCachedLines {
		clear(c.entries)
	}
	c.entries[key] = cachedLine{text: text, expires: now.Add(LineReuse)}
}
