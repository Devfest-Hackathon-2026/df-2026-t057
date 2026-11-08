package shortcut

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
)

// Lines は相棒の一言（AI）。AI が使えなければ空文字を返す。
type Lines interface {
	ShortcutLine(ctx context.Context, uid string, moment companion.Moment, taskID string) (string, error)
}

// Wallet は視聴時間の口座を、書き込みのあるトランザクションの中で最新にして読む（wallet.Service が実装する）。
type Wallet interface {
	Open(ctx context.Context, tx *store.Tx, uid string, now time.Time) (*wallet.Account, error)
}

// ZoneSource は本人のタイムゾーン。
type ZoneSource interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
}
