package profile

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
)

// CalendarSync は、タイムゾーンか言語が変わったときに、カレンダーの予定を書き直す（Outbox に積む）。
type CalendarSync interface {
	ProfileFormatChanged(ctx context.Context, tx *store.Tx, uid string) error
}
