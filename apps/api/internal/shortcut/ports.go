package shortcut

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// Lines は相棒の一言（AI）。AI が使えなければ空文字を返す。
type Lines interface {
	AppOpenLine(ctx context.Context, uid, taskID string) (string, error)
}

// Profiles は本人のタイムゾーンと言語（profile.Service が実装する）。
type Profiles interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
	Locale(ctx context.Context, tx *store.Tx, uid string) (locale.Locale, error)
}
