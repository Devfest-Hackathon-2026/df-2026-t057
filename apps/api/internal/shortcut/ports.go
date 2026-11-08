package shortcut

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// Lines は相棒の一言（AI）。AI が使えなければ空文字を返す。
type Lines interface {
	AppOpenLine(ctx context.Context, uid, taskID string) (string, error)
	BalanceZeroLine(ctx context.Context, uid, taskID string) (string, error)
}

// Wallet は口座を最新にしてから読む（wallet.Service が実装する）。
type Wallet interface {
	Read(ctx context.Context, uid string, fn func(ctx context.Context, tx *store.Tx, a *wallet.Account) error) error
}

// ZoneSource は本人のタイムゾーン。
type ZoneSource interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
}
