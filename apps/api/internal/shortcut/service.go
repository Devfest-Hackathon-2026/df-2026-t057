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
	runner   *store.Runner
	clock    clock.Clock
	tasks    *task.Service
	wallet   Wallet
	profiles Profiles
	lines    Lines
	appURL   string
	log      *slog.Logger
	limiter  limiter
}

// New は Service を作る。appURL は通知から開く管理画面のオリジン。
func New(runner *store.Runner, c clock.Clock, tasks *task.Service, wallets Wallet, profiles Profiles, lines Lines, appURL string, log *slog.Logger) *Service {
	return &Service{runner: runner, clock: c, tasks: tasks, wallet: wallets, profiles: profiles, lines: lines, appURL: appURL, log: log}
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

// 呼び出しの上限を数える枠。聞き直しは別に数え、開閉を連打しても通知が抜けないようにする。
const (
	bucketOpenClose = "open"
	bucketCheck     = "check"
)

// authorize は呼び出しの上限とキーを確かめ、アカウントの uid を返す。アカウントを作らない。
// 全体の上限 → キーの照合 → アカウントごとの上限の順に確かめる（不正なキーでほかの人の上限を使い切れないように）。
// キーの照合とアカウントごとの上限は、fn と同じトランザクションの中で行う。
func (s *Service) authorize(ctx context.Context, bucket, key string, now time.Time, fn func(ctx context.Context, tx *store.Tx, uid string) error) (string, error) {
	uid, hash, ok := ParseKey(key)
	if !ok || !store.ValidUID(uid) {
		return "", apperr.Unauthenticated("shortcut.key_malformed")
	}
	if !s.limiter.allow(bucket+":*", TotalPerMinute, now) {
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
		if !s.limiter.allow(bucket+":"+uid, PerAccountPerMinute, now) {
			return apperr.RateLimited("shortcut.rate_limited", time.Minute)
		}
		return fn(ctx, tx, uid)
	})
	return uid, err
}

// due は、開いた時刻と今の口座・次の課題から、通知する時刻を決める。
func (s *Service) due(ctx context.Context, uid string, opened time.Time, next *task.Task) (Due, bool, error) {
	var account wallet.Account
	if err := s.wallet.Read(ctx, uid, func(_ context.Context, _ *store.Tx, a *wallet.Account) error {
		account = *a
		return nil
	}); err != nil {
		return Due{}, false, err
	}
	var start time.Time
	if next != nil {
		start = next.Plan.Start
	}
	due, found := NextDue(opened, account.Available(), account.Mode.Limited(), start)
	return due, found, nil
}

// Opened は YouTube アプリを開いたときの応答。Wait が 0 なら、ショートカットは待たずに終わる。
type Opened struct {
	OpenID string
	Wait   time.Duration
}

// Open は YouTube アプリが前面に来たときにショートカットから呼ばれる。開いた時刻を記録するだけで、通知はしない。
// 通知する時刻（持ち時間を使い切る時刻か、課題を始める時刻）があれば、聞き直すまでの待ち時間を返す。
func (s *Service) Open(ctx context.Context, key string) (Opened, error) {
	now := store.Truncate(s.clock.Now())
	var next *task.Task
	uid, err := s.authorize(ctx, bucketOpenClose, key, now, func(ctx context.Context, tx *store.Tx, uid string) error {
		var err error
		if next, err = s.tasks.NextPending(ctx, tx, uid); err != nil {
			return err
		}
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil {
			return err
		}
		return tx.Set(opensRef(uid), opens.Record(now))
	})
	if err != nil {
		return Opened{}, err
	}
	due, found, err := s.due(ctx, uid, now, next)
	if err != nil {
		return Opened{}, err
	}
	return Opened{OpenID: FormatOpenID(now), Wait: FirstWait(due, found, now)}, nil
}

// Notification は通知に出す内容。OpenApp が true なら、ショートカットは URL（DOPAGAKI）も開く。
type Notification struct {
	Title   string
	Message string
	URL     string
	OpenApp bool
}

// Checked は聞き直したときの応答。Action が ActionNotify なら Notification、ActionWait なら Wait が入る。
type Checked struct {
	Action       Action
	Wait         time.Duration
	Notification Notification
}

// Check は、開いてから待ち終わったショートカットから呼ばれる。
// その「開いた」がまだ続いていて通知の時刻になっていれば、相棒の一言を返す。時刻が後ろにずれていれば待ち直させる。
// もう閉じた、もっと新しい「開いた」がある、もう通知した、のどれかなら何もしない（AI も使わない）。
func (s *Service) Check(ctx context.Context, key, openID string) (Checked, error) {
	opened, ok := ParseOpenID(openID)
	if !ok {
		return Checked{}, apperr.Invalid("request.invalid", apperr.Field("openId", "not_allowed"))
	}
	now := s.clock.Now()
	var next *task.Task
	var lang locale.Locale
	watching := false
	uid, err := s.authorize(ctx, bucketCheck, key, now, func(ctx context.Context, tx *store.Tx, uid string) error {
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil || !opens.Watching(opened) {
			return err
		}
		watching = true
		if next, err = s.tasks.NextPending(ctx, tx, uid); err != nil {
			return err
		}
		lang, err = s.profiles.Locale(ctx, tx, uid)
		return err
	})
	if err != nil || !watching {
		return Checked{Action: ActionNone}, err
	}
	due, found, err := s.due(ctx, uid, opened, next)
	if err != nil {
		return Checked{}, err
	}
	action, wait := Decide(due, found, now)
	if action != ActionNotify {
		return Checked{Action: action, Wait: wait}, nil
	}
	// 同じ回の聞き直しが重なっても、通知は1回だけにする。
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil || !opens.Watching(opened) {
			watching = false
			return err
		}
		return tx.Set(opensRef(uid), opens.MarkNotified(opened))
	})
	if err != nil || !watching {
		return Checked{Action: ActionNone}, err
	}
	return Checked{Action: ActionNotify, Notification: s.notification(ctx, uid, lang, due.Reason, next)}, nil
}

// notification は通知の内容。持ち時間を使い切ったときは DOPAGAKI も開く。
func (s *Service) notification(ctx context.Context, uid string, lang locale.Locale, reason DueReason, next *task.Task) Notification {
	n := Notification{Title: notificationTitles[lang], URL: s.appURL + "/"}
	taskID := ""
	if next != nil {
		taskID = next.ID
		n.URL += "?notification=" + url.QueryEscape(next.ID)
	}
	var line string
	var err error
	switch reason {
	case DueBalance:
		n.OpenApp = true
		line, err = s.lines.BalanceZeroLine(ctx, uid, taskID)
		if line == "" {
			line = fallbackBalanceZeroLine(lang)
		}
	case DueTask:
		line, err = s.lines.NoticeLine(ctx, uid, taskID)
		if line == "" {
			line = fallbackTaskLine(lang, next.Title, next.Why)
		}
	}
	if err != nil {
		s.log.WarnContext(ctx, "shortcut line failed", "reason", string(reason), "code", apperr.As(err).Code)
	}
	n.Message = line
	return n
}

// Close は YouTube アプリが前面から外れたときにショートカットから呼ばれる。
// 直前に開いてから使った時間を振り返り用に記録するだけで、通知はしない。視聴時間の残高は変えない。
func (s *Service) Close(ctx context.Context, key string) error {
	now := store.Truncate(s.clock.Now())
	_, err := s.authorize(ctx, bucketOpenClose, key, now, func(ctx context.Context, tx *store.Tx, uid string) error {
		opens, _, err := store.Get[AppOpens](ctx, tx, opensRef(uid))
		if err != nil {
			return err
		}
		updated, _, ok := opens.RecordClose(now)
		if !ok {
			return nil
		}
		return tx.Set(opensRef(uid), updated)
	})
	return err
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
	loc, err := s.profiles.Location(ctx, tx, uid)
	if err != nil {
		return 0, 0, err
	}
	today, week = opens.UseMinutes(now, loc)
	return today, week, nil
}
