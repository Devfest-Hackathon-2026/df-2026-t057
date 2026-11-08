package wallet

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Service は視聴時間のユースケース。付与・消費のメソッドはトランザクションを受け取り、ほかの機能の変更と一緒に確定する。
type Service struct {
	runner *store.Runner
	clock  clock.Clock
	zones  ZoneSource
	nights NightWindow
	work   ActiveWork
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, zones ZoneSource, nights NightWindow, work ActiveWork) *Service {
	return &Service{runner: runner, clock: c, zones: zones, nights: nights, work: work}
}

func accountRef(uid string) store.Ref         { return store.UserDoc(uid, "wallet") }
func ledgerCollection(uid string) string      { return store.UserCollection(uid, "ledger") }
func ledgerRef(uid, entryID string) store.Ref { return store.UserItem(uid, "ledger", entryID) }

// Open は口座を読み、無ければ作り、日付が進んでいれば日を切り替える（書き込みのあるトランザクションで使う）。
func (s *Service) Open(ctx context.Context, tx *store.Tx, uid string, now time.Time) (*Account, error) {
	a, found, err := store.Get[Account](ctx, tx, accountRef(uid))
	if err != nil {
		return nil, err
	}
	loc, err := s.zones.Location(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	if !found {
		created, initial := NewAccount(now, loc)
		if err := tx.Set(ledgerRef(uid, initial.ID), initial); err != nil {
			return nil, err
		}
		if err := s.save(ctx, tx, uid, &created); err != nil {
			return nil, err
		}
		a = created
	}
	if a.Rollover(now, loc) {
		_, night, err := s.nights.Active(ctx, tx, uid, now)
		if err != nil {
			return nil, err
		}
		if e, ok := a.Refill(now, night); ok {
			if _, err := s.appendOnce(ctx, tx, uid, &a, e); err != nil {
				return nil, err
			}
		}
		if err := s.save(ctx, tx, uid, &a); err != nil {
			return nil, err
		}
	}
	return &a, nil
}

// needsWrite は、読むだけでは済まない（口座の作成か日の切り替えが要る）か。
func (s *Service) needsWrite(ctx context.Context, tx *store.Tx, uid string, now time.Time) (bool, error) {
	a, found, err := store.Get[Account](ctx, tx, accountRef(uid))
	if err != nil || !found {
		return !found, err
	}
	loc, err := s.zones.Location(ctx, tx, uid)
	if err != nil {
		return false, err
	}
	return LocalDay(now, loc) > a.Day, nil
}

// Read は口座を最新にしてから、読み取り専用のトランザクションで fn を呼ぶ。
// 口座の作成や日の切り替えが要るときは、先に別のトランザクションで確定する（fn の中で台帳を検索できるように）。
func (s *Service) Read(ctx context.Context, uid string, fn func(ctx context.Context, tx *store.Tx, a *Account) error) error {
	now := s.clock.Now()
	write := false
	if err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		write, err = s.needsWrite(ctx, tx, uid, now)
		return err
	}); err != nil {
		return err
	}
	if write {
		if err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
			_, err := s.Open(ctx, tx, uid, now)
			return err
		}); err != nil {
			return err
		}
	}
	return s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		a, err := s.Open(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		return fn(ctx, tx, a)
	})
}

// Get は口座と版を返す。
func (s *Service) Get(ctx context.Context, uid string) (a Account, rev int64, err error) {
	err = s.Read(ctx, uid, func(ctx context.Context, tx *store.Tx, current *Account) error {
		a = *current
		rev, err = tx.Revision(ctx, uid, store.ResourceWallet)
		return err
	})
	return a, rev, err
}

func (s *Service) save(ctx context.Context, tx *store.Tx, uid string, a *Account) error {
	if err := tx.Set(accountRef(uid), a); err != nil {
		return err
	}
	_, err := tx.Touch(ctx, uid, store.ResourceWallet)
	return err
}

// appendOnce は項目を台帳に追記して口座に反映する。同じ ID の項目があれば何もせず、既存の項目を返す。
func (s *Service) appendOnce(ctx context.Context, tx *store.Tx, uid string, a *Account, e Entry) (Entry, error) {
	existing, found, err := store.Get[Entry](ctx, tx, ledgerRef(uid, e.ID))
	if err != nil {
		return Entry{}, err
	}
	if found {
		return existing, nil
	}
	e.At = store.Truncate(e.At)
	e = a.Apply(e)
	if err := tx.Set(ledgerRef(uid, e.ID), e); err != nil {
		return Entry{}, err
	}
	return e, s.save(ctx, tx, uid, a)
}

func (s *Service) night(ctx context.Context, tx *store.Tx, uid string, now time.Time) (bool, error) {
	_, active, err := s.nights.Active(ctx, tx, uid, now)
	return active, err
}

func (s *Service) grant(ctx context.Context, tx *store.Tx, uid string, kind Kind, key string, seconds int, now time.Time, meta Entry) (Entry, error) {
	if existing, found, err := store.Get[Entry](ctx, tx, ledgerRef(uid, EntryID(kind, key))); err != nil || found {
		return existing, err
	}
	a, err := s.Open(ctx, tx, uid, now)
	if err != nil {
		return Entry{}, err
	}
	night, err := s.night(ctx, tx, uid, now)
	if err != nil {
		return Entry{}, err
	}
	e := a.Grant(kind, key, seconds, now, night)
	e.TaskID, e.TaskTitle = meta.TaskID, meta.TaskTitle
	return s.appendOnce(ctx, tx, uid, a, e)
}

// GrantRegistration は課題の登録の報酬（1課題に1回）を付与し、付与した秒数を返す。
func (s *Service) GrantRegistration(ctx context.Context, tx *store.Tx, uid, taskID, title string, now time.Time) (int, error) {
	e, err := s.grant(ctx, tx, uid, KindRegistration, taskID, RegistrationRewardSeconds, now, Entry{TaskID: taskID, TaskTitle: title})
	return e.Seconds, err
}

// GrantSleepBonus は睡眠のボーナス（1晩に1回）を付与し、付与した秒数を返す。
func (s *Service) GrantSleepBonus(ctx context.Context, tx *store.Tx, uid, night string, seconds int, now time.Time) (int, error) {
	e, err := s.grant(ctx, tx, uid, KindSleep, night, seconds, now, Entry{})
	return e.Seconds, err
}

// ReleaseDeferred は保留中の報酬を、上限の範囲で受け取る。key はべき等キー（夜の日付など）。
func (s *Service) ReleaseDeferred(ctx context.Context, tx *store.Tx, uid, key string, now time.Time) (int, error) {
	if existing, found, err := store.Get[Entry](ctx, tx, ledgerRef(uid, EntryID(KindRelease, key))); err != nil || found {
		return existing.Seconds, err
	}
	a, err := s.Open(ctx, tx, uid, now)
	if err != nil {
		return 0, err
	}
	e, ok := a.Release(key, now)
	if !ok {
		return 0, nil
	}
	e, err = s.appendOnce(ctx, tx, uid, a, e)
	return e.Seconds, err
}

// WorkResult は作業の記録の結果。
type WorkResult struct {
	SessionID     string
	TaskID        string
	WorkSeconds   int
	RewardSeconds int
	Deferred      bool
}

func workResult(sessionID string, e Entry) WorkResult {
	return WorkResult{SessionID: sessionID, TaskID: e.TaskID, WorkSeconds: e.WorkSeconds, RewardSeconds: e.Seconds, Deferred: e.Deferred}
}

// WorkRecorded は、このセッションの作業を記録済みならその結果を返す。
func (s *Service) WorkRecorded(ctx context.Context, tx *store.Tx, uid, sessionID string) (WorkResult, bool, error) {
	e, found, err := store.Get[Entry](ctx, tx, ledgerRef(uid, EntryID(KindWork, sessionID)))
	if err != nil || !found {
		return WorkResult{}, false, err
	}
	return workResult(sessionID, e), true, nil
}

// RecordWork は作業を記録し、報酬を計算する。同じセッションは一度だけ記録する。
func (s *Service) RecordWork(ctx context.Context, tx *store.Tx, uid, sessionID, taskID, title string, intervals []Interval, now time.Time) (WorkResult, error) {
	if r, found, err := s.WorkRecorded(ctx, tx, uid, sessionID); err != nil || found {
		return r, err
	}
	a, err := s.Open(ctx, tx, uid, now)
	if err != nil {
		return WorkResult{}, err
	}
	night, err := s.night(ctx, tx, uid, now)
	if err != nil {
		return WorkResult{}, err
	}
	e, err := s.appendOnce(ctx, tx, uid, a, a.WorkReward(sessionID, taskID, title, a.TodaysWork(intervals), now, night))
	return workResult(sessionID, e), err
}

// ViewingRecorded は、この受付の視聴を記録済みか。
func (s *Service) ViewingRecorded(ctx context.Context, tx *store.Tx, uid, receipt string) (bool, error) {
	_, found, err := store.Get[Entry](ctx, tx, ledgerRef(uid, EntryID(KindViewing, receipt)))
	return found, err
}

// RecordViewing は視聴した秒数を記録する（受付ごとに一度だけ）。減らした秒数を返す。
func (s *Service) RecordViewing(ctx context.Context, tx *store.Tx, uid, receipt string, seconds int, now time.Time) (int, error) {
	a, err := s.Open(ctx, tx, uid, now)
	if err != nil {
		return 0, err
	}
	e, err := s.appendOnce(ctx, tx, uid, a, a.Viewing(receipt, max(0, seconds), now))
	return -e.Seconds, err
}

// UpdatePolicy はルールを変える。残高と今日の集計は変えない。作業中は 409 work.active。
func (s *Service) UpdatePolicy(ctx context.Context, uid string, mode Mode, dailyLimit int, ifMatch int64) (a Account, rev int64, err error) {
	var fields []apperr.FieldError
	if !mode.Valid() {
		fields = append(fields, apperr.Field("mode", "not_allowed"))
	}
	if !ValidDailyLimit(dailyLimit) {
		fields = append(fields, apperr.Field("dailyLimitSeconds", "not_allowed"))
	}
	if len(fields) > 0 {
		return Account{}, 0, apperr.Invalid("request.invalid", fields...)
	}
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceWallet, ifMatch); err != nil {
			return err
		}
		active, err := s.work.HasActive(ctx, tx, uid)
		if err != nil {
			return err
		}
		if active {
			return apperr.Conflict("work.active")
		}
		current, err := s.Open(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		current.Mode, current.DailyLimit = mode, dailyLimit
		if err := s.save(ctx, tx, uid, current); err != nil {
			return err
		}
		a = *current
		rev, err = tx.Revision(ctx, uid, store.ResourceWallet)
		return err
	})
	return a, rev, err
}

// Ledger は台帳を新しい順に返す。before を指定すると、その項目より前を返す。続きがあれば next にその ID を入れる。
func (s *Service) Ledger(ctx context.Context, uid string, limit int, before string) (entries []Entry, next string, err error) {
	if limit < 1 || limit > 200 {
		return nil, "", apperr.Invalid("request.invalid", apperr.Field("limit", "out_of_range"))
	}
	err = s.Read(ctx, uid, func(ctx context.Context, tx *store.Tx, _ *Account) error {
		q := store.Query{Collection: ledgerCollection(uid), OrderBy: "seq", Desc: true, Limit: limit + 1}
		if before != "" {
			anchor, found, err := store.Get[Entry](ctx, tx, ledgerRef(uid, before))
			if err != nil {
				return err
			}
			if !found {
				return apperr.NotFound("not_found")
			}
			q.Filters = []store.Filter{{Field: "seq", Op: store.Lt, Value: anchor.Seq}}
		}
		var err error
		entries, err = store.List[Entry](ctx, tx, q)
		return err
	})
	if len(entries) > limit {
		entries = entries[:limit]
		next = entries[limit-1].ID
	}
	return entries, next, err
}

// EntriesSince は since 以降の台帳の項目（記録の集計用）。
func (s *Service) EntriesSince(ctx context.Context, tx *store.Tx, uid string, since time.Time) ([]Entry, error) {
	return store.List[Entry](ctx, tx, store.Query{Collection: ledgerCollection(uid), Filters: []store.Filter{{Field: "at", Op: store.Ge, Value: since}}, OrderBy: "at"})
}

// Entries は台帳のすべての項目（作り直しと検証用）。
func (s *Service) Entries(ctx context.Context, tx *store.Tx, uid string) ([]Entry, error) {
	return store.List[Entry](ctx, tx, store.Query{Collection: ledgerCollection(uid), OrderBy: "seq"})
}

// Repair は口座の残高・保留・連番・今日の集計を台帳から作り直す（口座がずれたときの回復用）。
func (s *Service) Repair(ctx context.Context, uid string) (Account, error) {
	if err := s.Read(ctx, uid, func(context.Context, *store.Tx, *Account) error { return nil }); err != nil {
		return Account{}, err
	}
	var a Account
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		entries, err := s.Entries(ctx, tx, uid)
		if err != nil {
			return err
		}
		current, _, err := store.Get[Account](ctx, tx, accountRef(uid))
		if err != nil {
			return err
		}
		p := Rebuild(entries)
		current.Balance, current.Deferred, current.LastSeq = p.Balance, p.Deferred, p.LastSeq
		current.Today = p.Days[current.Day]
		current.Today.Day = current.Day
		a = current
		return s.save(ctx, tx, uid, &current)
	})
	return a, err
}
