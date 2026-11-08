package worksession

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

func sessionRef(uid string) store.Ref { return store.UserDoc(uid, "session") }

// Sessions は作業タイマーの保存場所。課題・視聴時間の機能から使うため、Service と分けて依存を持たない。
type Sessions struct{}

// Get は実行中の作業を返す。無ければ nil。
func (Sessions) Get(ctx context.Context, tx *store.Tx, uid string) (*Session, error) {
	s, found, err := store.Get[Session](ctx, tx, sessionRef(uid))
	if err != nil || !found {
		return nil, err
	}
	return &s, nil
}

// HasActive は作業中か。
func (x Sessions) HasActive(ctx context.Context, tx *store.Tx, uid string) (bool, error) {
	s, err := x.Get(ctx, tx, uid)
	return s != nil, err
}

// TaskRemoved は、消した課題のタイマーを記録せずに止める。
func (x Sessions) TaskRemoved(ctx context.Context, tx *store.Tx, uid, taskID string) error {
	s, err := x.Get(ctx, tx, uid)
	if err != nil || s == nil || s.TaskID != taskID {
		return err
	}
	return clear(ctx, tx, uid)
}

func clear(ctx context.Context, tx *store.Tx, uid string) error {
	if err := tx.Delete(sessionRef(uid)); err != nil {
		return err
	}
	_, err := tx.Touch(ctx, uid, store.ResourceWorkSession)
	return err
}

func save(ctx context.Context, tx *store.Tx, uid string, s Session) error {
	if err := tx.Set(sessionRef(uid), s); err != nil {
		return err
	}
	_, err := tx.Touch(ctx, uid, store.ResourceWorkSession)
	return err
}

// Service は作業タイマーのユースケース。
type Service struct {
	runner   *store.Runner
	clock    clock.Clock
	ids      id.Generator
	tasks    *task.Service
	wallet   *wallet.Service
	sessions Sessions
}

// New は Service を作る。
func New(runner *store.Runner, c clock.Clock, ids id.Generator, tasks *task.Service, w *wallet.Service) *Service {
	return &Service{runner: runner, clock: c, ids: ids, tasks: tasks, wallet: w}
}

// Snapshot は作業と、その時点の時刻。
type Snapshot struct {
	Session Session
	AsOf    time.Time
}

// Get は実行中の作業を返す。無ければ nil。
func (s *Service) Get(ctx context.Context, uid string) (*Snapshot, error) {
	now := s.clock.Now()
	var out *Snapshot
	err := s.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		x, err := s.sessions.Get(ctx, tx, uid)
		if x != nil {
			out = &Snapshot{Session: *x, AsOf: now}
		}
		return err
	})
	return out, err
}

// Start は作業を始める。同じ課題の作業中なら、タイマーはそのまま。ほかの課題の作業中なら task.another_active。
func (s *Service) Start(ctx context.Context, uid, taskID string) (Snapshot, error) {
	now := store.Truncate(s.clock.Now())
	var out Snapshot
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := s.sessions.Get(ctx, tx, uid)
		if err != nil {
			return err
		}
		if current != nil && current.TaskID != taskID {
			if _, err := s.tasks.LoadTx(ctx, tx, uid, taskID); err != nil {
				return err
			}
			return apperr.Conflict("task.another_active")
		}
		t, err := s.tasks.MarkActive(ctx, tx, uid, taskID, now)
		if err != nil {
			return err
		}
		if current == nil {
			started := Start(s.ids.New(), taskID, now, LimitFor(t.PlannedSeconds()))
			if err := save(ctx, tx, uid, started); err != nil {
				return err
			}
			current = &started
		}
		out = Snapshot{Session: *current, AsOf: now}
		return nil
	})
	return out, err
}

func (s *Service) change(ctx context.Context, uid string, fn func(x *Session, now time.Time) bool) (Snapshot, error) {
	now := store.Truncate(s.clock.Now())
	var out Snapshot
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		x, err := s.sessions.Get(ctx, tx, uid)
		if err != nil {
			return err
		}
		if x == nil {
			return apperr.Conflict("work.no_session")
		}
		if fn(x, now) {
			if err := save(ctx, tx, uid, *x); err != nil {
				return err
			}
		}
		out = Snapshot{Session: *x, AsOf: now}
		return nil
	})
	return out, err
}

// Pause は作業を一時停止する。一時停止中なら何もしない。
func (s *Service) Pause(ctx context.Context, uid string) (Snapshot, error) {
	return s.change(ctx, uid, func(x *Session, now time.Time) bool { return x.Pause(now) })
}

// Resume は一時停止した作業を再開する。動いているなら何もしない。
func (s *Service) Resume(ctx context.Context, uid string) (Snapshot, error) {
	return s.change(ctx, uid, func(x *Session, now time.Time) bool { return x.Resume(now) })
}

func intervals(x Session) []wallet.Interval {
	out := make([]wallet.Interval, len(x.Intervals))
	for i, iv := range x.Intervals {
		out[i] = wallet.Interval{Start: iv.Start, End: iv.End}
	}
	return out
}

// record はタイマーを閉じて作業を記録し、タイマーを片付ける。
func (s *Service) record(ctx context.Context, tx *store.Tx, uid string, x Session, now time.Time) (wallet.WorkResult, error) {
	x.Close(now)
	t, err := s.tasks.LoadTx(ctx, tx, uid, x.TaskID)
	if err != nil {
		return wallet.WorkResult{}, err
	}
	r, err := s.wallet.RecordWork(ctx, tx, uid, x.ID, x.TaskID, t.Title, intervals(x), now)
	if err != nil {
		return wallet.WorkResult{}, err
	}
	return r, clear(ctx, tx, uid)
}

// Confirm はここまでの作業を記録して報酬を計算し、課題を未開始に戻す。記録済みのセッションなら同じ結果を返す。
func (s *Service) Confirm(ctx context.Context, uid, sessionID string) (wallet.WorkResult, error) {
	now := store.Truncate(s.clock.Now())
	var out wallet.WorkResult
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		r, done, err := s.wallet.WorkRecorded(ctx, tx, uid, sessionID)
		if err != nil {
			return err
		}
		if done {
			out = r
			return nil
		}
		x, err := s.sessions.Get(ctx, tx, uid)
		if err != nil {
			return err
		}
		if x == nil {
			return apperr.Conflict("work.no_session")
		}
		if x.ID != sessionID {
			return apperr.Conflict("work.session_mismatch")
		}
		if out, err = s.record(ctx, tx, uid, *x, now); err != nil {
			return err
		}
		return s.tasks.MarkPending(ctx, tx, uid, x.TaskID)
	})
	return out, err
}

// Completed は完了の結果。Work は、この課題の作業中だったときだけ入る。
type Completed struct {
	Task task.Task
	Work *wallet.WorkResult
}

// Complete は課題を完了する。この課題の作業中なら、作業を記録して報酬を計算する。完了済みなら何もしない。
func (s *Service) Complete(ctx context.Context, uid, taskID string) (Completed, error) {
	now := store.Truncate(s.clock.Now())
	var out Completed
	err := s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		out = Completed{}
		t, err := s.tasks.LoadTx(ctx, tx, uid, taskID)
		if err != nil {
			return err
		}
		if t.Status == task.StatusCompleted {
			out.Task = t
			return nil
		}
		x, err := s.sessions.Get(ctx, tx, uid)
		if err != nil {
			return err
		}
		if x != nil && x.TaskID == taskID {
			r, err := s.record(ctx, tx, uid, *x, now)
			if err != nil {
				return err
			}
			out.Work = &r
		}
		out.Task, _, err = s.tasks.MarkCompleted(ctx, tx, uid, taskID, now)
		return err
	})
	return out, err
}
