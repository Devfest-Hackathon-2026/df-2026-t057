package shortcut

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
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
	runner  *store.Runner
	clock   clock.Clock
	tasks   *task.Service
	wallet  Wallet
	zones   ZoneSource
	lines   Lines
	appURL  string
	log     *slog.Logger
	limiter limiter
}

// New は Service を作る。appURL は通知から開く管理画面のオリジン。
func New(runner *store.Runner, c clock.Clock, tasks *task.Service, wallets Wallet, zones ZoneSource, lines Lines, appURL string, log *slog.Logger) *Service {
	return &Service{runner: runner, clock: c, tasks: tasks, wallet: wallets, zones: zones, lines: lines, appURL: appURL, log: log}
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
	// Stop は今日の視聴時間が0分で、ショートカットが DOPAGAKI を開くとき true（閉じたときは常に false）。
	Stop bool
	// MinutesLeft は今日あと何分見られるか。残高を使わないモード（reminder・self）と閉じたときは nil。
	MinutesLeft *int
}

// authorize は呼び出しの上限とキーを確かめ、アカウントの uid を返す。アカウントを作らない。
// 全体の上限 → キーの照合 → アカウントごとの上限の順に確かめる（不正なキーでほかの人の上限を使い切れないように）。
// キーの照合とアカウントごとの上限は、fn と同じトランザクションの中で行う。
func (s *Service) authorize(ctx context.Context, key string, now time.Time, fn func(ctx context.Context, tx *store.Tx, uid string) error) (string, error) {
	uid, hash, ok := ParseKey(key)
	if !ok || !store.ValidUID(uid) {
		return "", apperr.Unauthenticated("shortcut.key_malformed")
	}
	if !s.limiter.allow("*", TotalPerMinute, now) {
		return "", apperr.RateLimited("shortcut.rate_limited", time.Minute)
	}
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
		return fn(ctx, tx, uid)
	})
	return uid, err
}

// notificationURL は通知から開く管理画面。次の課題があれば、その課題の通知画面を開く。
func (s *Service) notificationURL(next *task.Task) string {
	if next == nil {
		return s.appURL + "/"
	}
	return s.appURL + "/?notification=" + url.QueryEscape(next.ID)
}

// Open は YouTube アプリが前面に来たときにショートカットから呼ばれる。
// 今日の残り時間と相棒の一言を返す。残り0分なら Stop を立て、ショートカットが DOPAGAKI を開く（YouTube は止めない）。
func (s *Service) Open(ctx context.Context, key string) (Notification, error) {
	now := s.clock.Now()
	var next *task.Task
	var account *wallet.Account
	uid, err := s.authorize(ctx, key, now, func(ctx context.Context, tx *store.Tx, uid string) error {
		var err error
		if next, err = s.tasks.NextPending(ctx, tx, uid); err != nil {
			return err
		}
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil {
			return err
		}
		if account, err = s.wallet.Open(ctx, tx, uid, now); err != nil {
			return err
		}
		return tx.Set(opensRef(uid), opens.Record(store.Truncate(now)))
	})
	if err != nil {
		return Notification{}, err
	}
	n := Notification{Title: notificationTitle, URL: s.notificationURL(next)}
	taskID, title, why := "", "", ""
	if next != nil {
		taskID, title, why = next.ID, next.Title, next.Why
	}
	limited := account.Mode.Limited()
	if limited && account.Available() <= 0 {
		zero := 0
		n.Stop, n.MinutesLeft = true, &zero
		n.Message = s.line(ctx, uid, companion.MomentBalanceZero, taskID, fallbackBalanceZeroLine())
		return n, nil
	}
	n.Message = s.line(ctx, uid, companion.MomentAppOpen, taskID, fallbackLine(title, why))
	if limited {
		minutes := max(1, account.Available()/60)
		n.MinutesLeft = &minutes
		n.Message = minutesLeftPrefix(minutes) + n.Message
	}
	return n, nil
}

// line は相棒の一言。AI が使えないときや失敗したときは fallback を返す。
func (s *Service) line(ctx context.Context, uid string, moment companion.Moment, taskID, fallback string) string {
	line, err := s.lines.ShortcutLine(ctx, uid, moment, taskID)
	if err != nil {
		s.log.WarnContext(ctx, "shortcut line failed", "moment", string(moment), "code", apperr.As(err).Code)
	}
	if line == "" {
		return fallback
	}
	return line
}

// Close は YouTube アプリが前面から外れたときにショートカットから呼ばれる。
// 直前に開いてから使った時間を記録し、振り返りの一言を返す。視聴時間の残高は変えない。
func (s *Service) Close(ctx context.Context, key string) (Notification, error) {
	now := s.clock.Now()
	var next *task.Task
	var use *AppUse
	_, err := s.authorize(ctx, key, now, func(ctx context.Context, tx *store.Tx, uid string) error {
		var err error
		if next, err = s.tasks.NextPending(ctx, tx, uid); err != nil {
			return err
		}
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil {
			return err
		}
		updated, recorded, ok := opens.RecordClose(store.Truncate(now))
		if !ok {
			return nil
		}
		use = &recorded
		return tx.Set(opensRef(uid), updated)
	})
	if err != nil {
		return Notification{}, err
	}
	title := ""
	if next != nil {
		title = next.Title
	}
	return Notification{Title: notificationTitle, Message: closeLine(use, title), URL: s.notificationURL(next)}, nil
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
	loc, err := s.zones.Location(ctx, tx, uid)
	if err != nil {
		return 0, 0, true, err
	}
	today, week = opens.Counts(now, loc)
	return today, week, true, nil
}

// AppUseMinutes は YouTube アプリを使った分数（今日と直近7日）。ショートカットを設定していなければ 0。
func (s *Service) AppUseMinutes(ctx context.Context, tx *store.Tx, uid string, now time.Time) (today, week int, err error) {
	_, configured, err := store.Get[Key](ctx, tx, keyRef(uid))
	if err != nil || !configured {
		return 0, 0, err
	}
	opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
	if err != nil {
		return 0, 0, err
	}
	loc, err := s.zones.Location(ctx, tx, uid)
	if err != nil {
		return 0, 0, err
	}
	today, week = opens.UseMinutes(now, loc)
	return today, week, nil
}
