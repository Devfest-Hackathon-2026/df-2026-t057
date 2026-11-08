package profile

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Service は本人の情報と設定のユースケース。
type Service struct {
	runner   *store.Runner
	clock    clock.Clock
	calendar CalendarSync
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, calendar CalendarSync) *Service {
	return &Service{runner: runner, clock: c, calendar: calendar}
}

func profileRef(uid string) store.Ref  { return store.UserDoc(uid, "profile") }
func settingsRef(uid string) store.Ref { return store.UserDoc(uid, "settings") }

// LoadLocation は IANA のタイムゾーンを読み込む。読み込めなければ UTC。
func LoadLocation(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func validZone(name string) bool {
	if name == "" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// ProfileTx はトランザクションの中で本人の情報を読む。
func (s *Service) ProfileTx(ctx context.Context, tx *store.Tx, uid string) (Profile, error) {
	p, found, err := store.Get[Profile](ctx, tx, profileRef(uid))
	if err != nil {
		return Profile{}, err
	}
	if !found {
		return Default(), nil
	}
	return p, nil
}

// Location は本人のタイムゾーン。日付の区切りはすべてこれで決める。
func (s *Service) Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error) {
	p, err := s.ProfileTx(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	return LoadLocation(p.TimeZone), nil
}

// Get は本人の情報と版を返す。
func (s *Service) Get(ctx context.Context, uid string) (p Profile, rev int64, err error) {
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if p, err = s.ProfileTx(ctx, tx, uid); err != nil {
			return err
		}
		rev, err = tx.Revision(ctx, uid, store.ResourceProfile)
		return err
	})
	return p, rev, err
}

// Update は本人の情報を変える。ifMatch は取得したときの版。
func (s *Service) Update(ctx context.Context, uid string, in Input, ifMatch int64) (p Profile, rev int64, err error) {
	if err := ValidateInput(in, validZone(in.TimeZone)); err != nil {
		return Profile{}, 0, err
	}
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceProfile, ifMatch); err != nil {
			return err
		}
		current, err := s.ProfileTx(ctx, tx, uid)
		if err != nil {
			return err
		}
		p = current.Apply(in, now)
		if err := tx.Set(profileRef(uid), p); err != nil {
			return err
		}
		if p.TimeZone != current.TimeZone {
			if err := s.calendar.ProfileZoneChanged(ctx, tx, uid); err != nil {
				return err
			}
		}
		rev, err = tx.Touch(ctx, uid, store.ResourceProfile)
		return err
	})
	return p, rev, err
}

// Remember は作業傾向に1行加える（相談の「覚える」カード）。同じ内容が既にあれば何も変えない。
func (s *Service) Remember(ctx context.Context, uid, text string) (p Profile, rev int64, err error) {
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := s.ProfileTx(ctx, tx, uid)
		if err != nil {
			return err
		}
		if p, err = current.Remember(text); err != nil {
			return err
		}
		if p.Context == current.Context {
			rev, err = tx.Revision(ctx, uid, store.ResourceProfile)
			return err
		}
		if err := tx.Set(profileRef(uid), p); err != nil {
			return err
		}
		rev, err = tx.Touch(ctx, uid, store.ResourceProfile)
		return err
	})
	return p, rev, err
}

// SettingsTx はトランザクションの中で設定を読む。
func (s *Service) SettingsTx(ctx context.Context, tx *store.Tx, uid string) (Settings, error) {
	v, found, err := store.Get[Settings](ctx, tx, settingsRef(uid))
	if err != nil {
		return Settings{}, err
	}
	if !found {
		return DefaultSettings(), nil
	}
	return v, nil
}

// GetSettings は設定と版を返す。
func (s *Service) GetSettings(ctx context.Context, uid string) (v Settings, rev int64, err error) {
	err = s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		if v, err = s.SettingsTx(ctx, tx, uid); err != nil {
			return err
		}
		rev, err = tx.Revision(ctx, uid, store.ResourceSettings)
		return err
	})
	return v, rev, err
}

// UpdateSettings は設定を変える。スヌーズの長さが変わると、計画の版を上げる（確認中のスヌーズ案を無効にする）。
func (s *Service) UpdateSettings(ctx context.Context, uid string, in Settings, ifMatch int64) (v Settings, rev int64, err error) {
	if err := ValidateSettings(in); err != nil {
		return Settings{}, 0, err
	}
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.Expect(ctx, uid, store.ResourceSettings, ifMatch); err != nil {
			return err
		}
		current, err := s.SettingsTx(ctx, tx, uid)
		if err != nil {
			return err
		}
		if err := tx.Set(settingsRef(uid), in); err != nil {
			return err
		}
		if current.SnoozeMinutes != in.SnoozeMinutes {
			// 計画の版は platform の meta にあるため、課題の機能を呼ばずに上げられる。
			if _, err := tx.Touch(ctx, uid, store.ResourceTasks); err != nil {
				return err
			}
		}
		v = in
		rev, err = tx.Touch(ctx, uid, store.ResourceSettings)
		return err
	})
	return v, rev, err
}

// Context は作業傾向と、それを書き換えた時刻（AI に渡す範囲を決める）。
func (s *Service) Context(ctx context.Context, tx *store.Tx, uid string) (string, *time.Time, error) {
	p, err := s.ProfileTx(ctx, tx, uid)
	return p.Context, p.ContextSince, err
}

// SnoozeMinutes はスヌーズの長さ。
func (s *Service) SnoozeMinutes(ctx context.Context, tx *store.Tx, uid string) (int, error) {
	v, err := s.SettingsTx(ctx, tx, uid)
	return v.SnoozeMinutes, err
}

func (s *Service) Forget(ctx context.Context, uid string) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		p, err := s.ProfileTx(ctx, tx, uid)
		if err != nil {
			return err
		}
		now := store.Truncate(s.clock.Now())
		p.Context = ""
		p.ContextSince = &now
		if err := tx.Set(profileRef(uid), p); err != nil {
			return err
		}
		_, err = tx.Touch(ctx, uid, store.ResourceProfile)
		return err
	})
}
