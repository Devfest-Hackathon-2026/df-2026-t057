// Package outbox は、Firestore の変更と外部サービス（Google カレンダー）への反映を整合させる仕組み。
//
//  1. 状態を変えるトランザクションの中で、users/{uid}/outbox/{jobId} にジョブを書く（Enqueue）。
//  2. 確定した後に Dispatcher がジョブを登録する（live は Cloud Tasks、demo は何もしない）。
//  3. Cloud Tasks が POST /api/internal/outbox/{jobId} を呼び、Run がジョブを実行する。ジョブの処理はべき等にする。
//     失敗すると 5xx を返して Cloud Tasks に再試行させる。MaxAttempts 回失敗したジョブは failed として残す。
package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Kind はジョブの種類。
type Kind string

const (
	KindTaskUpsert  Kind = "calendar.task.upsert"
	KindTaskDelete  Kind = "calendar.task.delete"
	KindSleepUpsert Kind = "calendar.sleep.upsert"
	KindSleepDelete Kind = "calendar.sleep.delete"
	KindSync        Kind = "calendar.sync"
	// KindExport は計画の変更（予定の説明の DOPAGAKI ブロック）をまとめて書き直す。
	KindExport Kind = "calendar.export"
)

// Status はジョブの状態。
type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	// StatusCancelled は、実行する前に不要になったジョブ（カレンダー連携の解除）。Cloud Tasks から呼ばれても何もしない。
	StatusCancelled Status = "cancelled"
)

// MaxAttempts はジョブを失敗とみなすまでの試行回数。Cloud Tasks のキューの上限（deploy/terraform/outbox.tf）より小さくする。
const MaxAttempts = 8

// DoneTTL は完了したジョブを残す期間（TTL で消す）。
const DoneTTL = 7 * 24 * time.Hour

// Job は外部へ反映するジョブ。
type Job struct {
	ID string `json:"id" firestore:"id"`
	// UID は Cloud Tasks から呼ばれたときに利用者を特定するために持つ。
	UID       string     `json:"uid" firestore:"uid"`
	Kind      Kind       `json:"kind" firestore:"kind"`
	Subject   string     `json:"subject,omitempty" firestore:"subject,omitempty"`
	Status    Status     `json:"status" firestore:"status"`
	Attempts  int        `json:"attempts" firestore:"attempts"`
	LastError string     `json:"lastError,omitempty" firestore:"lastError,omitempty"`
	CreatedAt time.Time  `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt" firestore:"updatedAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty" firestore:"expiresAt,omitempty"`
}

// Dispatcher は確定したジョブを実行の仕組みに渡す。
type Dispatcher interface {
	Dispatch(ctx context.Context, uid, jobID string) error
}

// Handler はジョブを実行する。べき等にする。
type Handler func(ctx context.Context, job Job) error

// Collection はジョブのコレクション名。
const Collection = "outbox"

func ref(uid, jobID string) store.Ref { return store.UserItem(uid, Collection, jobID) }

// Outbox はジョブの登録と実行を受け持つ。
type Outbox struct {
	runner     *store.Runner
	dispatcher Dispatcher
	clock      clock.Clock
	ids        id.Generator
	handlers   map[Kind]Handler
	log        *slog.Logger
}

// New は Outbox を作る。
func New(runner *store.Runner, d Dispatcher, c clock.Clock, ids id.Generator, log *slog.Logger) *Outbox {
	return &Outbox{runner: runner, dispatcher: d, clock: c, ids: ids, handlers: map[Kind]Handler{}, log: log}
}

// Handle はジョブの種類ごとの処理を登録する。起動時にだけ呼ぶ。
func (o *Outbox) Handle(kind Kind, h Handler) { o.handlers[kind] = h }

// Enqueue はトランザクションの中でジョブを書き、確定した後に Dispatcher に渡す。
func (o *Outbox) Enqueue(ctx context.Context, tx *store.Tx, uid string, kind Kind, subject string) (Job, error) {
	now := o.clock.Now()
	job := Job{ID: o.ids.New(), UID: uid, Kind: kind, Subject: subject, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := tx.Set(ref(uid, job.ID), job); err != nil {
		return Job{}, err
	}
	if _, err := tx.Touch(ctx, uid, store.ResourceCalendar); err != nil {
		return Job{}, err
	}
	tx.AfterCommit(func() { o.dispatch(context.WithoutCancel(ctx), uid, job.ID) })
	return job, nil
}

func (o *Outbox) dispatch(ctx context.Context, uid, jobID string) {
	if err := o.dispatcher.Dispatch(ctx, uid, jobID); err != nil {
		// ジョブは pending のまま残る。Redispatch（毎日の更新）で登録し直す。
		o.log.ErrorContext(ctx, "outbox dispatch failed", "job", jobID, "error_type", fmt.Sprintf("%T", err))
	}
}

// Redispatch は、登録から時間がたっても pending のジョブを登録し直す（Dispatcher の失敗からの回復）。
func (o *Outbox) Redispatch(ctx context.Context, uid string, olderThan time.Duration) error {
	var jobs []Job
	err := o.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		jobs, err = store.List[Job](ctx, tx, store.Query{Collection: store.UserCollection(uid, Collection), Filters: []store.Filter{{Field: "status", Op: store.Eq, Value: string(StatusPending)}}})
		return err
	})
	if err != nil {
		return err
	}
	cutoff := o.clock.Now().Add(-olderThan)
	for _, j := range jobs {
		if j.UpdatedAt.Before(cutoff) {
			o.dispatch(ctx, uid, j.ID)
		}
	}
	return nil
}

// Run はジョブを実行する。実行済み・失敗済みなら何もしない。再試行すべき失敗はエラーを返す。
func (o *Outbox) Run(ctx context.Context, uid, jobID string) error {
	var job Job
	var found bool
	err := o.runner.View(ctx, func(ctx context.Context, tx *store.Tx) error {
		var err error
		job, found, err = store.Get[Job](ctx, tx, ref(uid, jobID))
		return err
	})
	if err != nil {
		return err
	}
	if !found || job.Status != StatusPending {
		return nil
	}
	h, ok := o.handlers[job.Kind]
	if !ok {
		return o.finish(ctx, job, apperr.Internal(fmt.Errorf("outbox: no handler for %s", job.Kind)), true)
	}
	runErr := h(ctx, job)
	return o.finish(ctx, job, runErr, false)
}

func (o *Outbox) finish(ctx context.Context, job Job, runErr error, permanent bool) error {
	now := o.clock.Now()
	failed := false
	err := o.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, found, err := store.Get[Job](ctx, tx, ref(job.UID, job.ID))
		if err != nil || !found || current.Status != StatusPending {
			return err
		}
		current.UpdatedAt = now
		if runErr == nil {
			expires := now.Add(DoneTTL)
			current.Status, current.LastError, current.ExpiresAt = StatusDone, "", &expires
		} else {
			current.Attempts++
			current.LastError = apperr.As(runErr).Code
			if permanent || current.Attempts >= MaxAttempts {
				current.Status, failed = StatusFailed, true
			}
		}
		if err := tx.Set(ref(job.UID, job.ID), current); err != nil {
			return err
		}
		if job.Kind == KindSleepUpsert || job.Kind == KindSleepDelete {
			// 睡眠の calendarPending が変わるので、睡眠の版も上げる（クライアントが取り直せるように）。
			if _, err := tx.Touch(ctx, job.UID, store.ResourceSleep); err != nil {
				return err
			}
		}
		_, err = tx.Touch(ctx, job.UID, store.ResourceCalendar)
		return err
	})
	if err != nil {
		return err
	}
	if runErr != nil {
		o.log.WarnContext(ctx, "outbox job failed", "job", job.ID, "kind", job.Kind, "attempt", job.Attempts+1, "code", apperr.As(runErr).Code, "gave_up", failed)
		if !failed {
			return runErr
		}
	}
	return nil
}

// Cancel は利用者の pending・failed のジョブをすべて取り消す（連携の解除）。取り消したジョブは DoneTTL の後に消える。
// 取り消した数を返す。同じトランザクションの中で、このコレクションを検索した後に呼ぶ。
func (o *Outbox) Cancel(ctx context.Context, tx *store.Tx, uid string) (int, error) {
	now := o.clock.Now()
	expires := now.Add(DoneTTL)
	cancelled := 0
	for _, s := range []Status{StatusPending, StatusFailed} {
		jobs, err := store.List[Job](ctx, tx, store.Query{Collection: store.UserCollection(uid, Collection), Filters: []store.Filter{{Field: "status", Op: store.Eq, Value: string(s)}}})
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			j.Status, j.UpdatedAt, j.ExpiresAt = StatusCancelled, now, &expires
			if err := tx.Set(ref(uid, j.ID), j); err != nil {
				return 0, err
			}
			cancelled++
		}
	}
	if cancelled > 0 {
		if _, err := tx.Touch(ctx, uid, store.ResourceCalendar); err != nil {
			return 0, err
		}
	}
	return cancelled, nil
}

// Counts は pending と failed のジョブの数。
func Counts(ctx context.Context, tx *store.Tx, uid string) (pending, failed int, err error) {
	for _, s := range []Status{StatusPending, StatusFailed} {
		jobs, err := store.List[Job](ctx, tx, store.Query{Collection: store.UserCollection(uid, Collection), Filters: []store.Filter{{Field: "status", Op: store.Eq, Value: string(s)}}})
		if err != nil {
			return 0, 0, err
		}
		if s == StatusPending {
			pending = len(jobs)
		} else {
			failed = len(jobs)
		}
	}
	return pending, failed, nil
}

// HasPending は、ある種類・対象の pending のジョブがあるか。
func HasPending(ctx context.Context, tx *store.Tx, uid string, kinds []Kind, subject string) (bool, error) {
	jobs, err := store.List[Job](ctx, tx, store.Query{Collection: store.UserCollection(uid, Collection), Filters: []store.Filter{{Field: "status", Op: store.Eq, Value: string(StatusPending)}}})
	if err != nil {
		return false, err
	}
	for _, j := range jobs {
		for _, k := range kinds {
			if j.Kind == k && (subject == "" || j.Subject == subject) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Retry resets failed jobs and redispatches them only after the transaction commits.
func (o *Outbox) Retry(ctx context.Context, uid string) error {
	return o.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		jobs, err := store.List[Job](ctx, tx, store.Query{Collection: store.UserCollection(uid, Collection), Filters: []store.Filter{{Field: "status", Op: store.Eq, Value: string(StatusFailed)}}})
		if err != nil {
			return err
		}
		for _, j := range jobs {
			j.Status, j.Attempts, j.LastError = StatusPending, 0, ""
			j.UpdatedAt = o.clock.Now()
			if err := tx.Set(ref(uid, j.ID), j); err != nil {
				return err
			}
			tx.AfterCommit(func() { o.dispatch(context.WithoutCancel(ctx), uid, j.ID) })
		}
		_, err = tx.Touch(ctx, uid, store.ResourceCalendar)
		return err
	})
}
