package companion

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// Tasks は課題を読む（task.Service が実装する）。
type Tasks interface {
	All(ctx context.Context, tx *store.Tx, uid string) ([]task.Task, error)
	LoadTx(ctx context.Context, tx *store.Tx, uid, taskID string) (task.Task, error)
}

// Profiles は本人の情報を読む（profile.Service が実装する）。
type Profiles interface {
	ProfileTx(ctx context.Context, tx *store.Tx, uid string) (profile.Profile, error)
}

// Wallet は口座を最新にしてから読む（wallet.Service が実装する）。fn の中のトランザクションで他の機能も読む。
type Wallet interface {
	Read(ctx context.Context, uid string, fn func(ctx context.Context, tx *store.Tx, a *wallet.Account) error) error
}

// Sleep は睡眠の状態を読む（sleep.Service が実装する）。
type Sleep interface {
	GetTx(ctx context.Context, tx *store.Tx, uid string, now time.Time) (sleep.State, error)
}

// AppOpens は iPhone で YouTube アプリを開いた回数と使った分数（shortcut が実装する）。ショートカットを設定していなければ configured は false。
type AppOpens interface {
	AppOpenCounts(ctx context.Context, tx *store.Tx, uid string, now time.Time) (today, week int, configured bool, err error)
	// AppUseMinutes は開いてから閉じるまでの分数（今日と直近7日）。
	AppUseMinutes(ctx context.Context, tx *store.Tx, uid string, now time.Time) (today, week int, err error)
}

// Generator は AI で一言を作る（adapter/gemini が実装する）。呼ぶたびに AI の利用回数を1回使う。
// 返す文は信頼できない入力として、呼び出し側で検証する。
type Generator interface {
	Line(ctx context.Context, uid string, req LineRequest) (string, error)
}
