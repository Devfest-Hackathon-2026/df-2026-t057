package wallet

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// ZoneSource は本人のタイムゾーン（日付の区切りに使う）。
type ZoneSource interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
}

// NightWindow は、今が夜の時間帯か（夜の付与は朝まで保留する）。sleep が実装する。
type NightWindow interface {
	Active(ctx context.Context, tx *store.Tx, uid string, now time.Time) (night string, active bool, err error)
}

// ActiveWork は作業中かを返す（作業中はルールを変えられない）。worksession が実装する。
type ActiveWork interface {
	HasActive(ctx context.Context, tx *store.Tx, uid string) (bool, error)
}
