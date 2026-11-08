// Package idempotency は Idempotency-Key による再送対策。
//
// 流れ:
//  1. Begin: キーの記録を「処理中」で作る（同じキーの同時実行は 409 idempotency.in_progress、内容が違えば 409 idempotency.key_reused、
//     処理済みなら記録した応答をそのまま返す）。
//  2. 状態を変えるトランザクションは、コミットの直前に Hook が同じトランザクションの中で記録へ「反映済み」と書く。
//     これで、同じキーの変更が二度反映されることはない（報酬の付与などがちょうど1回になる）。
//  3. Complete: 応答（ステータス・ヘッダー・本文）を記録する。何も反映せずに失敗した応答は記録せず、記録を消す（同じキーで再試行できる）。
//
// 反映した直後に処理が止まり、応答を記録できなかった場合は、その後の同じキーの要求は 409 idempotency.in_progress になる
// （変更を二重に反映しないことを優先する）。記録は expiresAt を過ぎると Firestore の TTL で消える。
package idempotency

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

const (
	// TTL は記録を残す期間。
	TTL = 24 * time.Hour
	// Lease は処理中とみなす期間。AI の呼び出し（最大40秒程度）より長くする。
	Lease = 2 * time.Minute
)

// State は記録の状態。
type State string

const (
	StateInProgress State = "in_progress"
	StateDone       State = "done"
)

// Record は Idempotency-Key ごとの記録。
type Record struct {
	Key        string            `json:"key" firestore:"key"`
	Hash       string            `json:"hash" firestore:"hash"`
	State      State             `json:"state" firestore:"state"`
	Token      string            `json:"token" firestore:"token"`
	LeaseUntil time.Time         `json:"leaseUntil" firestore:"leaseUntil"`
	Applied    bool              `json:"applied" firestore:"applied"`
	AppliedBy  string            `json:"appliedBy,omitempty" firestore:"appliedBy,omitempty"`
	Status     int               `json:"status,omitempty" firestore:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty" firestore:"headers,omitempty"`
	Body       []byte            `json:"body,omitempty" firestore:"body,omitempty"`
	CreatedAt  time.Time         `json:"createdAt" firestore:"createdAt"`
	ExpiresAt  time.Time         `json:"expiresAt" firestore:"expiresAt"`
}

// Attempt は処理中の要求。context に入れて、Hook と Complete に渡す。
type Attempt struct {
	UID   string
	Key   string
	Hash  string
	Token string
}

type attemptKey struct{}

// WithAttempt は context に処理中の要求を入れる。
func WithAttempt(ctx context.Context, a Attempt) context.Context {
	return context.WithValue(ctx, attemptKey{}, a)
}

// AttemptFrom は context から処理中の要求を取り出す。
func AttemptFrom(ctx context.Context) (Attempt, bool) {
	a, ok := ctx.Value(attemptKey{}).(Attempt)
	return a, ok
}

// Service は記録を読み書きする。
type Service struct {
	runner *store.Runner
	clock  clock.Clock
	ids    id.Generator
}

// New は Service を作り、runner にコミット直前の処理を登録する。
func New(runner *store.Runner, c clock.Clock, ids id.Generator) *Service {
	s := &Service{runner: runner, clock: c, ids: ids}
	runner.AddHook(s.markApplied)
	return s
}

func ref(uid, key string) store.Ref { return store.UserItem(uid, "idempotency", key) }

// Begin は要求の処理を始める。処理済みなら replay に記録を返す。
func (s *Service) Begin(ctx context.Context, uid, key, hash string) (attempt Attempt, replay *Record, err error) {
	now := s.clock.Now()
	err = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		rec, found, err := store.Get[Record](ctx, tx, ref(uid, key))
		if err != nil {
			return err
		}
		if found && now.Before(rec.ExpiresAt) {
			if rec.Hash != hash {
				return apperr.Conflict("idempotency.key_reused")
			}
			if rec.State == StateDone {
				replay = &rec
				return nil
			}
			if now.Before(rec.LeaseUntil) {
				return apperr.Conflict("idempotency.in_progress")
			}
			// 前の処理の期限が切れたので引き継ぐ。前の処理が反映済みなら、Hook が二重の反映を止める。
			rec.Token, rec.LeaseUntil = s.ids.New(), now.Add(Lease)
		} else {
			rec = Record{Key: key, Hash: hash, State: StateInProgress, Token: s.ids.New(), LeaseUntil: now.Add(Lease), CreatedAt: now, ExpiresAt: now.Add(TTL)}
		}
		attempt = Attempt{UID: uid, Key: key, Hash: hash, Token: rec.Token}
		return tx.Set(ref(uid, key), rec)
	})
	return attempt, replay, err
}

// markApplied は、要求に伴う状態の変更と同じトランザクションで「反映済み」と記録する。
// 利用者のリソースの版を上げるトランザクションだけを対象にする（AI の利用量の記録などは対象外）。
func (s *Service) markApplied(ctx context.Context, tx *store.Tx) error {
	a, ok := AttemptFrom(ctx)
	if !ok || !tx.Touched(a.UID) {
		return nil
	}
	rec, found, err := store.Get[Record](ctx, tx, ref(a.UID, a.Key))
	if err != nil {
		return err
	}
	if !found || rec.Token != a.Token || (rec.Applied && rec.AppliedBy != a.Token) {
		return apperr.Conflict("idempotency.in_progress")
	}
	if rec.Applied {
		return nil
	}
	rec.Applied, rec.AppliedBy = true, a.Token
	return tx.Set(ref(a.UID, a.Key), rec)
}

// Complete は応答を記録する。何も反映せずに失敗した応答（4xx・5xx）は記録せず、同じキーで再試行できるようにする。
func (s *Service) Complete(ctx context.Context, a Attempt, status int, headers map[string]string, body []byte) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		rec, found, err := store.Get[Record](ctx, tx, ref(a.UID, a.Key))
		if err != nil || !found || rec.Token != a.Token {
			return err
		}
		if !rec.Applied && status >= 400 {
			return tx.Delete(ref(a.UID, a.Key))
		}
		rec.State, rec.Status, rec.Headers, rec.Body = StateDone, status, headers, body
		return tx.Set(ref(a.UID, a.Key), rec)
	})
}
