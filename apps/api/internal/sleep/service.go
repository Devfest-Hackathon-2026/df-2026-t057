package sleep

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

func sleepRef(uid string) store.Ref { return store.UserDoc(uid, "sleep") }

func load(ctx context.Context, tx *store.Tx, uid string) (*Sleep, error) {
	s, found, err := store.Get[Sleep](ctx, tx, sleepRef(uid))
	if err != nil || !found {
		return nil, err
	}
	return &s, nil
}

// Nights は夜の時間帯を答える。ほかの機能（視聴時間・視聴）から使うため、Service と分けて依存を持たない。
type Nights struct {
	zones ZoneSource
}

// NewNights は Nights を作る。
func NewNights(zones ZoneSource) *Nights { return &Nights{zones: zones} }

// Active は now が夜の時間帯なら、その夜の日付を返す。
func (n *Nights) Active(ctx context.Context, tx *store.Tx, uid string, now time.Time) (string, bool, error) {
	s, err := load(ctx, tx, uid)
	if err != nil || s == nil {
		return "", false, err
	}
	loc, err := n.zones.Location(ctx, tx, uid)
	if err != nil {
		return "", false, err
	}
	night, ok := s.NightAt(now, loc)
	return night.Key, ok, nil
}

// MarkWatched は、夜の時間帯に Shorts を見たことを記録する（その夜はボーナスなし）。
func (n *Nights) MarkWatched(ctx context.Context, tx *store.Tx, uid string, now time.Time) error {
	s, err := load(ctx, tx, uid)
	if err != nil || s == nil {
		return err
	}
	loc, err := n.zones.Location(ctx, tx, uid)
	if err != nil {
		return err
	}
	night, ok := s.NightAt(now, loc)
	if !ok || s.WatchedNight == night.Key {
		return nil
	}
	s.WatchedNight = night.Key
	if err := tx.Set(sleepRef(uid), s); err != nil {
		return err
	}
	_, err = tx.Touch(ctx, uid, store.ResourceSleep)
	return err
}

// Service は睡眠のユースケース。
type Service struct {
	runner   *store.Runner
	clock    clock.Clock
	zones    ZoneSource
	wallet   *wallet.Service
	calendar CalendarSync
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, zones ZoneSource, w *wallet.Service, calendar CalendarSync) *Service {
	return &Service{runner: runner, clock: c, zones: zones, wallet: w, calendar: calendar}
}

// State は睡眠の設定と、今（または次）の夜。
type State struct {
	Sleep           *Sleep
	Night           Night
	NightActive     bool
	CalendarPending bool
	Revision        int64
}

func (s *Service) state(ctx context.Context, tx *store.Tx, uid string, now time.Time) (State, error) {
	st := State{}
	var err error
	if st.Sleep, err = load(ctx, tx, uid); err != nil {
		return State{}, err
	}
	if st.Revision, err = tx.Revision(ctx, uid, store.ResourceSleep); err != nil {
		return State{}, err
	}
	if st.Sleep == nil {
		return st, nil
	}
	loc, err := s.zones.Location(ctx, tx, uid)
	if err != nil {
		return State{}, err
	}
	st.Night, st.NightActive = st.Sleep.CurrentOrNext(now, loc)
	st.CalendarPending, err = s.calendar.SleepPending(ctx, tx, uid)
	return st, err
}

// Get は睡眠の状態を返す。
func (s *Service) Get(ctx context.Context, uid string) (st State, err error) {
	now := s.clock.Now()
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		st, err = s.state(ctx, tx, uid, now)
		return err
	})
	return st, err
}

// GetTx はトランザクションの中で睡眠の状態を返す（相談などから使う）。
func (s *Service) GetTx(ctx context.Context, tx *store.Tx, uid string, now time.Time) (State, error) {
	return s.state(ctx, tx, uid, now)
}

// Update は就寝と起床の時刻を設定する。時刻が変わると、その夜から判定をやり直す。
func (s *Service) Update(ctx context.Context, uid string, next Schedule, ifMatch int64) (st State, err error) {
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceSleep, ifMatch); err != nil {
			return err
		}
		current, err := load(ctx, tx, uid)
		if err != nil {
			return err
		}
		loc, err := s.zones.Location(ctx, tx, uid)
		if err != nil {
			return err
		}
		saved := Set(current, next, store.Truncate(now), loc)
		if err := tx.Set(sleepRef(uid), saved); err != nil {
			return err
		}
		if current == nil || current.Schedule != next {
			if err := s.calendar.SleepChanged(ctx, tx, uid); err != nil {
				return err
			}
		}
		if _, err := tx.Touch(ctx, uid, store.ResourceSleep); err != nil {
			return err
		}
		st, err = s.state(ctx, tx, uid, now)
		return err
	})
	return st, err
}

// Clear は睡眠の設定をやめる。朝まで保留していた報酬は、上限の範囲で受け取る。
func (s *Service) Clear(ctx context.Context, uid string, ifMatch int64) (st State, err error) {
	now := s.clock.Now()
	releaseKey := "clear-" + now.UTC().Format("20060102T150405.000000")
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceSleep, ifMatch); err != nil {
			return err
		}
		current, err := load(ctx, tx, uid)
		if err != nil {
			return err
		}
		if current != nil {
			if _, err := s.wallet.ReleaseDeferred(ctx, tx, uid, releaseKey, now); err != nil {
				return err
			}
			if err := tx.Delete(sleepRef(uid)); err != nil {
				return err
			}
			if err := s.calendar.SleepCleared(ctx, tx, uid); err != nil {
				return err
			}
			if _, err := tx.Touch(ctx, uid, store.ResourceSleep); err != nil {
				return err
			}
		}
		st, err = s.state(ctx, tx, uid, now)
		return err
	})
	return st, err
}

// Morning は朝の精算の結果。
type Morning struct {
	Night           string
	Kept            bool
	BonusSeconds    int
	ReleasedSeconds int
}

// SettleMorning は前の夜を精算する。精算するものが無ければ nil を返し、何も書き込まない。
func (s *Service) SettleMorning(ctx context.Context, uid string) (*Morning, error) {
	now := s.clock.Now()
	pending := false
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := load(ctx, tx, uid)
		if err != nil {
			return err
		}
		loc, err := s.zones.Location(ctx, tx, uid)
		if err != nil {
			return err
		}
		_, pending = Settle(current, now, loc)
		return nil
	})
	if err != nil || !pending {
		return nil, err
	}
	var morning *Morning
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		morning = nil
		current, err := load(ctx, tx, uid)
		if err != nil {
			return err
		}
		loc, err := s.zones.Location(ctx, tx, uid)
		if err != nil {
			return err
		}
		settlement, ok := Settle(current, now, loc)
		if !ok {
			return nil
		}
		m := &Morning{Night: settlement.Night.Key, Kept: settlement.Kept}
		if m.ReleasedSeconds, err = s.wallet.ReleaseDeferred(ctx, tx, uid, settlement.Night.Key, now); err != nil {
			return err
		}
		if settlement.Kept {
			if m.BonusSeconds, err = s.wallet.GrantSleepBonus(ctx, tx, uid, settlement.Night.Key, BonusSeconds, now); err != nil {
				return err
			}
		}
		if err := tx.Set(sleepRef(uid), settlement.Apply(*current)); err != nil {
			return err
		}
		if _, err := tx.Touch(ctx, uid, store.ResourceSleep); err != nil {
			return err
		}
		morning = m
		return nil
	})
	return morning, err
}
