package profile

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// CalendarSync は、タイムゾーンが変わったときに就寝の予定をカレンダーへ反映し直す（Outbox に積む）。
type CalendarSync interface {
	ProfileZoneChanged(ctx context.Context, tx *store.Tx, uid string) error
}
