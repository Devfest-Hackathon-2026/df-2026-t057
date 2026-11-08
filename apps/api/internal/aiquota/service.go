package aiquota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// collection は回数を保存するコレクション（利用者のデータとは分け、ドキュメント ID に uid をそのまま使わない）。
const collection = "aiUsage"

// takeTimeout は回数の確保にかける最長の時間。
const takeTimeout = 5 * time.Second

// Quota は AI の利用回数を確保する。
type Quota struct {
	runner *store.Runner
	clock  clock.Clock
	limits Limits
}

// New は Quota を作る。
func New(runner *store.Runner, c clock.Clock, limits Limits) *Quota {
	return &Quota{runner: runner, clock: c, limits: limits}
}

func globalRef() store.Ref { return store.GlobalDoc(collection, "global") }

func userRef(uid string) store.Ref {
	sum := sha256.Sum256([]byte(uid))
	return store.GlobalDoc(collection, "user-"+hex.EncodeToString(sum[:]))
}

// Take は uid の AI 呼び出しを1回分確保する。上限なら ai.rate_limited（Retry-After 付き）、
// 確保できなければ ai.unavailable を返す（AI を呼ばない）。
func (q *Quota) Take(ctx context.Context, uid string) error {
	if uid == "" || !q.limits.Valid() {
		return apperr.Unavailable("ai.unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, takeTimeout)
	defer cancel()
	now := q.clock.Now()
	err := q.runner.Update(ctx, func(ctx context.Context, tx *store.Tx) error {
		// 全体のドキュメントを先に読み、同時に確保する利用者どうしでロックの順を揃える。
		global, _, err := store.Get[Usage](ctx, tx, globalRef())
		if err != nil {
			return err
		}
		user, _, err := store.Get[Usage](ctx, tx, userRef(uid))
		if err != nil {
			return err
		}
		nextUser, nextGlobal, retryAt, ok := Reserve(user, global, q.limits, now)
		if !ok {
			return apperr.RateLimited("ai.rate_limited", RetryAfter(retryAt, now))
		}
		if err := tx.Set(globalRef(), nextGlobal); err != nil {
			return err
		}
		return tx.Set(userRef(uid), nextUser)
	})
	if err == nil {
		return nil
	}
	var e *apperr.Error
	if errors.As(err, &e) && e.Kind == apperr.KindRateLimited {
		return e
	}
	return apperr.Unavailable("ai.unavailable").Wrap(err)
}
