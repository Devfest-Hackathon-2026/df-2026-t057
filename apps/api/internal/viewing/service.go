package viewing

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

func stateRef(uid string) store.Ref        { return store.UserDoc(uid, "viewing") }
func sessionsCollection(uid string) string { return store.UserCollection(uid, "viewingSessions") }
func sessionRef(uid, id string) store.Ref  { return store.UserItem(uid, "viewingSessions", id) }

// Service は視聴のユースケース。
type Service struct {
	runner *store.Runner
	clock  clock.Clock
	wallet *wallet.Service
	nights Nights
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, w *wallet.Service, nights Nights) *Service {
	return &Service{runner: runner, clock: c, wallet: w, nights: nights}
}

// View は視聴の状態。
type View struct {
	Session         *Session
	Available       int
	Balance         int
	RequiresSession bool
	ReportCursor    int
	AcceptedSeconds *int
}

func (s *Service) load(ctx context.Context, tx *store.Tx, uid string) (State, *Session, error) {
	st, _, err := store.Get[State](ctx, tx, stateRef(uid))
	if err != nil || st.CurrentID == "" {
		return st, nil, err
	}
	x, found, err := store.Get[Session](ctx, tx, sessionRef(uid, st.CurrentID))
	if err != nil || !found {
		return st, nil, err
	}
	return st, &x, nil
}

func view(a *wallet.Account, current *Session, st State) View {
	cursor := st.FreeCursor
	if a.Mode.RequiresSession() {
		cursor = 0
		if current != nil {
			cursor = current.ConsumedSeconds
		}
	}
	return View{Session: current, Available: a.Available(), Balance: a.Balance, RequiresSession: a.Mode.RequiresSession(), ReportCursor: cursor}
}

// Get は視聴の状態を返す。
func (s *Service) Get(ctx context.Context, uid string) (v View, err error) {
	err = s.wallet.Read(ctx, uid, func(ctx context.Context, tx *store.Tx, a *wallet.Account) error {
		st, current, err := s.load(ctx, tx, uid)
		v = view(a, current, st)
		return err
	})
	return v, err
}

func (s *Service) touch(ctx context.Context, tx *store.Tx, uid string) error {
	_, err := tx.Touch(ctx, uid, store.ResourceViewing)
	return err
}

// Start は区切りを始める。id は区切りの ID（Idempotency-Key）。同じ ID なら何もしない。
func (s *Service) Start(ctx context.Context, uid, id string, seconds int, extendFrom string) (v View, err error) {
	if !ValidChoice(seconds, wallet.ViewingChoicesSeconds) {
		return View{}, apperr.Invalid("request.invalid", apperr.Field("seconds", "not_allowed"))
	}
	now := store.Truncate(s.clock.Now())
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		a, err := s.wallet.Open(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		st, current, err := s.load(ctx, tx, uid)
		if err != nil {
			return err
		}
		if _, exists, err := store.Get[Session](ctx, tx, sessionRef(uid, id)); err != nil || exists {
			v = view(a, current, st)
			return err
		}
		if err := CheckStart(current, extendFrom, a.Available()); err != nil {
			return err
		}
		x := Session{ID: id, ExtendFrom: extendFrom, StartedAt: now, LastReportAt: now, PlannedSeconds: min(seconds, a.Available())}
		st.CurrentID = id
		if err := tx.Set(sessionRef(uid, id), x); err != nil {
			return err
		}
		if err := tx.Set(stateRef(uid), st); err != nil {
			return err
		}
		v = view(a, &x, st)
		return s.touch(ctx, tx, uid)
	})
	return v, err
}

// End は区切りを終える。終わった区切りなら何もしない。
func (s *Service) End(ctx context.Context, uid, id string, reason EndReason) (v View, err error) {
	if !reason.Valid() {
		return View{}, apperr.Invalid("request.invalid", apperr.Field("reason", "not_allowed"))
	}
	now := store.Truncate(s.clock.Now())
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		a, err := s.wallet.Open(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		st, current, err := s.load(ctx, tx, uid)
		if err != nil {
			return err
		}
		if current == nil || current.ID != id {
			if _, exists, err := store.Get[Session](ctx, tx, sessionRef(uid, id)); err != nil || !exists {
				if err == nil {
					err = apperr.NotFound("viewing.session_unknown")
				}
				return err
			}
			v = view(a, current, st)
			return nil
		}
		if current.Active() {
			current.EndedAt, current.EndReason = &now, reason
			if err := tx.Set(sessionRef(uid, id), current); err != nil {
				return err
			}
			if err := s.touch(ctx, tx, uid); err != nil {
				return err
			}
		}
		v = view(a, current, st)
		return nil
	})
	return v, err
}

// Report は視聴した秒数を報告する。receipt（Idempotency-Key）ごとに一度だけ減らす。
func (s *Service) Report(ctx context.Context, uid, receipt string, seconds int, sessionID string) (v View, err error) {
	return s.report(ctx, uid, receipt, seconds, sessionID, nil)
}

// ReportFrom は計測区間を指定する報告。従来の秒数だけの報告も引き続き受け付ける。
func (s *Service) ReportFrom(ctx context.Context, uid, receipt string, seconds int, sessionID string, from int) (View, error) {
	if from < 0 || from > 2147483582 {
		return View{}, apperr.Invalid("request.invalid", apperr.Field("fromSeconds", "out_of_range"))
	}
	return s.report(ctx, uid, receipt, seconds, sessionID, &from)
}

func (s *Service) report(ctx context.Context, uid, receipt string, seconds int, sessionID string, from *int) (v View, err error) {
	if seconds < 1 || seconds > MaxReportSeconds {
		return View{}, apperr.Invalid("request.invalid", apperr.Field("seconds", "out_of_range"))
	}
	now := store.Truncate(s.clock.Now())
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		a, err := s.wallet.Open(ctx, tx, uid, now)
		if err != nil {
			return err
		}
		st, current, err := s.load(ctx, tx, uid)
		if err != nil {
			return err
		}
		done, err := s.wallet.ViewingRecorded(ctx, tx, uid, receipt)
		if err != nil {
			return err
		}
		if done {
			v = view(a, current, st)
			if from != nil {
				cursor := v.ReportCursor
				if a.Mode.RequiresSession() && (current == nil || current.ID != sessionID) {
					old, _, err := store.Get[Session](ctx, tx, sessionRef(uid, sessionID))
					if err != nil {
						return err
					}
					cursor = old.ConsumedSeconds
				}
				accepted := min(seconds, max(0, cursor-*from))
				v.AcceptedSeconds = &accepted
			}
			return nil
		}
		var charged, accepted int
		if a.Mode.RequiresSession() {
			if current == nil || current.ID != sessionID || !current.Active() {
				return apperr.Conflict("viewing.session_required")
			}
			if from == nil {
				charged = Charge(current, seconds, a.Available(), now)
			} else {
				charged, accepted, err = ChargeRange(current, *from, seconds, a.Available(), now)
				if err != nil {
					return err
				}
			}
			if err := tx.Set(sessionRef(uid, current.ID), current); err != nil {
				return err
			}
		} else {
			if from == nil {
				charged = ChargeFree(seconds, st.LastViewingAt, now)
				st.RecordFree(charged, now)
			} else {
				charged, accepted, err = ChargeFreeRange(&st, *from, seconds, now)
				if err != nil {
					return err
				}
			}
		}
		if _, err := s.wallet.RecordViewing(ctx, tx, uid, receipt, charged, now); err != nil {
			return err
		}
		if a, err = s.wallet.Open(ctx, tx, uid, now); err != nil {
			return err
		}
		st.LastViewingAt = &now
		if err := tx.Set(stateRef(uid), st); err != nil {
			return err
		}
		if charged > 0 {
			if err := s.nights.MarkWatched(ctx, tx, uid, now); err != nil {
				return err
			}
		}
		v = view(a, current, st)
		if from != nil {
			v.AcceptedSeconds = &accepted
		}
		return s.touch(ctx, tx, uid)
	})
	return v, err
}

// SessionsSince は since 以降に始めた区切り（記録の集計用）。
func (s *Service) SessionsSince(ctx context.Context, tx *store.Tx, uid string, since time.Time) ([]Session, error) {
	return store.List[Session](ctx, tx, store.Query{Collection: sessionsCollection(uid), Filters: []store.Filter{{Field: "startedAt", Op: store.Ge, Value: since}}, OrderBy: "startedAt"})
}
