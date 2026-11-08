package viewing

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Nights は夜の時間帯の記録（夜に見たらその夜のボーナスはなし）。sleep が実装する。
type Nights interface {
	MarkWatched(ctx context.Context, tx *store.Tx, uid string, now time.Time) error
}
