// Package idempotency は Idempotency-Key による再送対策。
//
// 設計: 状態の変更と応答の記録を、1つのトランザクションで確定する。
//
//  1. Begin: キーの記録を「処理中」で作る（同じキーの同時実行は 409 idempotency.in_progress、内容が違えば 409 idempotency.key_reused、
//     処理済みなら記録した応答をそのまま返す）。
//  2. Run: 要求ごとのトランザクション（store.Runner.Request）の中でハンドラーを実行する。ハンドラーが呼ぶサービスの Update は
//     すべてこのトランザクションに加わり、ハンドラーが作った応答（ステータス・ヘッダー・本文）を同じトランザクションで記録に書いて、
//     まとめて確定する。これで「変更は確定したが応答を記録できなかった」状態は起きない。報酬の付与などの変更は、
//     応答の記録と同時にちょうど1回だけ確定する。4xx・5xx の応答は何も確定せず記録も消す（同じキーで再試行できる）。
//     確定の前に処理が止まれば何も反映されず、Lease が切れた後の再送が最初から処理する。
//  3. Complete（従来の方式）: AI とのやり取りの間にトランザクションを開いたままにできない操作（相談）は、Run を使わずに
//     処理する。状態を変えるトランザクションのコミット直前に Hook が同じトランザクションの中で記録へ「反映済み」と書き、
//     処理が終わった後に Complete が応答を記録する。反映した直後に処理が止まって応答を記録できなかった場合、Lease が切れた後の
//     再送は処理をやり直し、Hook が二重の反映を止める（相談は返答済みなら何も書かずに同じ結果を返すので、正しい応答になる）。
//
// 記録は expiresAt を過ぎると Firestore の TTL で消える。
package idempotency

import (
	"context"
	"errors"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

const (
	// TTL は記録を残す期間。
	TTL = 24 * time.Hour
	// Lease は処理中とみなす期間。処理の途中で止まった要求を、この時間の後に再送で引き継ぐ。
	// AI の呼び出し（最大40秒程度）より長くする。
	Lease = 60 * time.Second
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

// Response は記録する応答。
type Response struct {
	Status  int
	Headers map[string]string
	Body    []byte
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

// errDiscard は、何も反映しない応答（4xx・5xx）で要求ごとのトランザクションを取り消すための印。
var errDiscard = errors.New("idempotency: discard")

// Run は要求を1つのトランザクションで処理する。fn が返した応答を、fn の中で加わった状態の変更と同じトランザクションで記録して
// 確定する。4xx・5xx の応答は何も確定せず、記録を消して（同じキーで再試行できる）その応答を返す。
// 確定に失敗したときは記録を消してエラーを返す（クライアントは同じキーで再送できる）。
func (s *Service) Run(ctx context.Context, a Attempt, fn func(ctx context.Context) Response) (Response, error) {
	var res Response
	err := s.runner.Request(ctx, a.UID, func(ctx context.Context, tx *store.Tx) error {
		res = fn(WithAttempt(ctx, a))
		if res.Status >= 400 {
			return errDiscard
		}
		rec, found, err := store.Get[Record](ctx, tx, ref(a.UID, a.Key))
		if err != nil {
			return err
		}
		if !found || rec.Token != a.Token {
			// 期限が切れて別の処理に引き継がれた。こちらの変更は確定しない。
			return apperr.Conflict("idempotency.in_progress")
		}
		rec.State, rec.Status, rec.Headers, rec.Body = StateDone, res.Status, res.Headers, res.Body
		rec.Applied, rec.AppliedBy = tx.Touched(a.UID), a.Token
		return tx.Set(ref(a.UID, a.Key), rec)
	})
	if err == nil {
		return res, nil
	}
	s.release(context.WithoutCancel(ctx), a)
	if errors.Is(err, errDiscard) {
		return res, nil
	}
	return Response{}, err
}

// release は、何も反映していない記録を消す（同じキーで再試行できるようにする）。
func (s *Service) release(ctx context.Context, a Attempt) {
	_ = s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		rec, found, err := store.Get[Record](ctx, tx, ref(a.UID, a.Key))
		if err != nil || !found || rec.Token != a.Token || rec.Applied {
			return err
		}
		return tx.Delete(ref(a.UID, a.Key))
	})
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

// Complete は従来の方式で応答を記録する（Run を使わない操作）。何も反映せずに失敗した応答（4xx・5xx）は記録せず、
// 同じキーで再試行できるようにする。反映した後に失敗した応答は記録せず、Lease が切れた後の再送に処理をやり直させる。
func (s *Service) Complete(ctx context.Context, a Attempt, res Response) error {
	return s.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		rec, found, err := store.Get[Record](ctx, tx, ref(a.UID, a.Key))
		if err != nil || !found || rec.Token != a.Token {
			return err
		}
		if res.Status >= 400 {
			if rec.Applied {
				return nil
			}
			return tx.Delete(ref(a.UID, a.Key))
		}
		rec.State, rec.Status, rec.Headers, rec.Body = StateDone, res.Status, res.Headers, res.Body
		return tx.Set(ref(a.UID, a.Key), rec)
	})
}
