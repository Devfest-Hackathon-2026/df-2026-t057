package assistant

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/worksession"
)

// Tasks は課題を読む（task.Service が実装する）。
type Tasks interface {
	All(ctx context.Context, tx *store.Tx, uid string) ([]task.Task, error)
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

// Sessions は実行中の作業を読む（worksession.Sessions が実装する）。
type Sessions interface {
	Get(ctx context.Context, tx *store.Tx, uid string) (*worksession.Session, error)
}

// AppOpens は iPhone で YouTube アプリを開いた回数と使った分数（shortcut が実装する）。ショートカットを設定していなければ configured は false。
type AppOpens interface {
	AppOpenCounts(ctx context.Context, tx *store.Tx, uid string, now time.Time) (today, week int, configured bool, err error)
	// AppUseMinutes は開いてから閉じるまでの分数（今日と直近7日）。
	AppUseMinutes(ctx context.Context, tx *store.Tx, uid string, now time.Time) (today, week int, err error)
}

// CalendarStatus はカレンダー連携の状態と、予定と結び付いた課題（cmd/server で calendar を合わせる）。
// nil ならカレンダーを使わない環境として扱う。
type CalendarStatus interface {
	Status(ctx context.Context, tx *store.Tx, uid string) (CalendarInfo, error)
}

// Model は相談の AI（adapter/gemini が実装する）。Consult は会話を始めるだけで、AI はまだ呼ばない。
type Model interface {
	Consult(in ConsultInput) Conversation
}

// Conversation は1回の相談の会話。Next を呼ぶたびに AI を1回呼び、利用回数を1回使う。
// results は前の Step の道具の結果（最初は空）。利用上限なら ai.rate_limited、失敗は ai.failed、読めない応答は ai.unreadable。
type Conversation interface {
	Next(ctx context.Context, uid string, results []ToolResult) (Step, error)
}
