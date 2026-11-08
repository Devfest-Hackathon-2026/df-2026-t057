package shortcut

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

func keyRef(uid string) store.Ref   { return store.UserDoc(uid, "shortcut") }
func opensRef(uid string) store.Ref { return store.UserDoc(uid, "appOpens") }

// limiter は1分間の呼び出しを数える（インスタンスごと）。
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *limiter) allow(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil || len(l.hits) > 10000 {
		l.hits = map[string][]time.Time{}
	}
	recent := l.hits[key][:0]
	for _, at := range l.hits[key] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= limit {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

// Service はショートカットのユースケース。
type Service struct {
	runner   *store.Runner
	clock    clock.Clock
	tasks    *task.Service
	profiles Profiles
	lines    Lines
	appURL   string
	log      *slog.Logger
	limiter  limiter
}

// New は Service を作る。appURL は通知から開く管理画面のオリジン。
func New(runner *store.Runner, c clock.Clock, tasks *task.Service, profiles Profiles, lines Lines, appURL string, log *slog.Logger) *Service {
	return &Service{runner: runner, clock: c, tasks: tasks, profiles: profiles, lines: lines, appURL: appURL, log: log}
}

// Status は接続キーを発行済みか。
func (s *Service) Status(ctx context.Context, uid string) (k *Key, err error) {
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		v, found, err := store.Get[Key](ctx, tx, keyRef(uid))
		if found {
			k = &v
		}
		return err
	})
	return k, err
}

// Issue は接続キーを発行する。キーはこの戻り値でだけ返し、以前のキーは使えなくなる。
func (s *Service) Issue(ctx context.Context, uid string) (string, error) {
	var b [32]byte
	_, _ = rand.Read(b[:])
	secret := hex.EncodeToString(b[:])
	now := store.Truncate(s.clock.Now())
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Set(keyRef(uid), Key{Hash: HashSecret(secret), CreatedAt: now}); err != nil {
			return err
		}
		_, err := tx.Touch(ctx, uid, store.ResourceShortcut)
		return err
	})
	if err != nil {
		return "", err
	}
	return FormatKey(uid, secret), nil
}

// Revoke は接続キーを無効にする。
func (s *Service) Revoke(ctx context.Context, uid string) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Delete(keyRef(uid)); err != nil {
			return err
		}
		_, err := tx.Touch(ctx, uid, store.ResourceShortcut)
		return err
	})
}

// Notification は通知に出す内容。
type Notification struct {
	Title   string
	Message string
	URL     string
}

// Open は YouTube を開いたときにショートカットから呼ばれる。アカウントを作らない。
// 全体の上限 → キーの照合 → アカウントごとの上限の順に確かめる（不正なキーでほかの人の上限を使い切れないように）。
func (s *Service) Open(ctx context.Context, key string) (Notification, error) {
	uid, hash, ok := ParseKey(key)
	if !ok || !store.ValidUID(uid) {
		return Notification{}, apperr.Unauthenticated("shortcut.key_malformed")
	}
	now := s.clock.Now()
	if !s.limiter.allow("*", TotalPerMinute, now) {
		return Notification{}, apperr.RateLimited("shortcut.rate_limited", time.Minute)
	}
	var next *task.Task
	var lang locale.Locale
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		k, found, err := store.Get[Key](ctx, tx, keyRef(uid))
		if err != nil {
			return err
		}
		if !found || !k.Matches(hash) {
			return apperr.Unauthenticated("shortcut.key_invalid")
		}
		if !s.limiter.allow(uid, PerAccountPerMinute, now) {
			return apperr.RateLimited("shortcut.rate_limited", time.Minute)
		}
		if next, err = s.tasks.NextPending(ctx, tx, uid); err != nil {
			return err
		}
		if lang, err = s.profiles.Locale(ctx, tx, uid); err != nil {
			return err
		}
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil {
			return err
		}
		return tx.Set(opensRef(uid), opens.Record(store.Truncate(now)))
	})
	if err != nil {
		return Notification{}, err
	}
	n := Notification{Title: notificationTitles[lang], URL: s.appURL + "/"}
	taskID, title, why := "", "", ""
	if next != nil {
		taskID, title, why = next.ID, next.Title, next.Why
		n.URL += "?notification=" + url.QueryEscape(next.ID)
	}
	line, err := s.lines.AppOpenLine(ctx, uid, taskID)
	if err != nil {
		s.log.WarnContext(ctx, "shortcut line failed", "code", apperr.As(err).Code)
	}
	if line == "" {
		line = fallbackLine(lang, title, why)
	}
	n.Message = line
	return n, nil
}

// AppOpenCounts は YouTube を開いた回数（今日と直近7日）。相棒の一言と相談の参考にする。
func (s *Service) AppOpenCounts(ctx context.Context, tx *store.Tx, uid string, now time.Time) (today, week int, configured bool, err error) {
	_, configured, err = store.Get[Key](ctx, tx, keyRef(uid))
	if err != nil || !configured {
		return 0, 0, configured, err
	}
	opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
	if err != nil {
		return 0, 0, true, err
	}
	loc, err := s.profiles.Location(ctx, tx, uid)
	if err != nil {
		return 0, 0, true, err
	}
	today, week = opens.Counts(now, loc)
	return today, week, true, nil
}
