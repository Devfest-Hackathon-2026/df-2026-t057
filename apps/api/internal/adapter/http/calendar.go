package http

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
)

// CalendarLink は calendar.Service を仕様の型に変換する。
type CalendarLink struct{ Service *calendar.Service }

func calendarStatusOut(s calendar.Status) oapi.CalendarStatus {
	out := oapi.CalendarStatus{Enabled: s.Enabled, Connected: s.Connected, Sync: oapi.CalendarStatusSync(s.Sync), LastSync: s.LastSync, FailedJobs: s.FailedJobs, Conflicts: ptr(s.Conflicts), PrimaryEvents: s.PrimaryEvents}
	if s.Reminders != nil {
		out.Reminders = ptr(s.Reminders)
	}
	return out
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

func (c CalendarLink) PrimaryEvents(ctx context.Context, uid string, from, to time.Time) ([]oapi.CalendarEvent, error) {
	events, err := c.Service.PrimaryEvents(ctx, uid, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]oapi.CalendarEvent, 0, len(events))
	for _, e := range events {
		v := oapi.CalendarEvent{Id: e.ID, Title: e.Title, AllDay: e.AllDay}
		if e.AllDay {
			v.StartDate, v.EndDate = &openapi_types.Date{Time: e.Start}, &openapi_types.Date{Time: e.End}
		} else {
			v.Start, v.End = ptr(e.Start), ptr(e.End)
		}
		out = append(out, v)
	}
	return out, nil
}

func (c CalendarLink) SetReminders(ctx context.Context, uid string, minutes []int) (oapi.CalendarStatus, error) {
	s, err := c.Service.SetReminders(ctx, uid, minutes)
	return calendarStatusOut(s), err
}

func (c CalendarLink) Webhook(ctx context.Context, channelID, token, state string) error {
	return c.Service.Webhook(ctx, channelID, token, state)
}

func (c CalendarLink) Renew(ctx context.Context) error { return c.Service.Renew(ctx) }

func (c CalendarLink) Conflicts(ctx context.Context, uid string) ([]calendar.Conflict, error) {
	return c.Service.Conflicts(ctx, uid)
}
func (c CalendarLink) Resolve(ctx context.Context, uid, id, choice, etag string, rev int64) error {
	return c.Service.Resolve(ctx, uid, id, choice, etag, rev)
}
func (c CalendarLink) Retry(ctx context.Context, uid string) (oapi.CalendarStatus, error) {
	s, err := c.Service.Retry(ctx, uid)
	return calendarStatusOut(s), err
}
