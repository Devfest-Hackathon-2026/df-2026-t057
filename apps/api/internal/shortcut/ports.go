package shortcut

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Lines は相棒の一言（AI）。AI が使えなければ空文字を返す。
type Lines interface {
	AppOpenLine(ctx context.Context, uid, taskID string) (string, error)
}

// ZoneSource は本人のタイムゾーン。
type ZoneSource interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
}
