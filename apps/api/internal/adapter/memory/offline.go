package memory

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// NoAI は AI を使わない環境（デモ・テスト）の task.Planner。すべて ai.unavailable を返し、課題の登録は逆算だけの計画になる。
type NoAI struct{}

func (NoAI) Detail(context.Context, string, task.DetailRequest) (task.DetailResult, error) {
	return task.DetailResult{}, apperr.Unavailable("ai.unavailable")
}

func (NoAI) Extract(context.Context, string, string, time.Time, *time.Location) ([]task.Input, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}

func (NoAI) Adjust(context.Context, string, task.AdjustRequest) ([]task.ChangeInput, error) {
	return nil, apperr.Unavailable("ai.unavailable")
}

// Dispatcher はデモの outbox.Dispatcher。デモはカレンダーに接続しないので、ジョブは作られない。
type Dispatcher struct{}

func (Dispatcher) Dispatch(context.Context, string, string) error { return nil }
