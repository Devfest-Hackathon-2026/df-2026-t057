package http

import (
	"context"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
)

// CalendarLink は calendar.Service を仕様の型に変換する。
type CalendarLink struct{ Service *calendar.Service }

func calendarStatusOut(s calendar.Status) oapi.CalendarStatus {
	return oapi.CalendarStatus{Enabled: s.Enabled, Connected: s.Connected, Sync: oapi.CalendarStatusSync(s.Sync), LastSync: s.LastSync, FailedJobs: s.FailedJobs}
}

func (c CalendarLink) Status(ctx context.Context, uid string) (oapi.CalendarStatus, error) {
	s, err := c.Service.Status(ctx, uid)
	return calendarStatusOut(s), err
}

func (c CalendarLink) Connect(ctx context.Context, uid, subject, email string) (string, error) {
	return c.Service.Connect(ctx, uid, subject, email)
}

func (c CalendarLink) Callback(ctx context.Context, state, code, errParam string) string {
	return c.Service.Callback(ctx, state, code, errParam)
}

func (c CalendarLink) SyncNow(ctx context.Context, uid string) (oapi.CalendarStatus, error) {
	s, err := c.Service.SyncNow(ctx, uid)
	return calendarStatusOut(s), err
}

func (c CalendarLink) Disconnect(ctx context.Context, uid string) (oapi.CalendarStatus, error) {
	s, err := c.Service.Disconnect(ctx, uid)
	return calendarStatusOut(s), err
}

func (c CalendarLink) Webhook(ctx context.Context, channelID, token, state string) error {
	return c.Service.Webhook(ctx, channelID, token, state)
}

func (c CalendarLink) Renew(ctx context.Context) error { return c.Service.Renew(ctx) }
