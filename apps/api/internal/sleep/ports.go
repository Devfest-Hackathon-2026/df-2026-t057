package sleep

import (
	"context"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// ZoneSource は本人のタイムゾーン。
type ZoneSource interface {
	Location(ctx context.Context, tx *store.Tx, uid string) (*time.Location, error)
}

// CalendarSync は就寝の予定をカレンダーへ反映する（Outbox に積む）。カレンダーを使わない環境では何もしない。
type CalendarSync interface {
	SleepChanged(ctx context.Context, tx *store.Tx, uid string) error
	SleepCleared(ctx context.Context, tx *store.Tx, uid string) error
	// SleepPending はカレンダーへの反映を待っているか。
	SleepPending(ctx context.Context, tx *store.Tx, uid string) (bool, error)
}
