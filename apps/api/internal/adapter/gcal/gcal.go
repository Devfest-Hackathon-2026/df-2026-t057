// Package gcal は calendar.Google を、Google の OAuth・Calendar API・Secret Manager で実装する。
//
// Google の失敗は、calendar が処理を分けるエラー（ErrEventGone など）か、apperr.External("external.failed") にして返す。
// 外部 API のレスポンス本文やトークンは、エラーにもログにも含めない。
package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	calendarapi "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// TokenStore は利用者の OAuth トークン（JSON）の保存場所。本番は Secret Manager（SecretStore）。
type TokenStore interface {
	// Save はトークンを保存し、前のトークンを破棄する。
	Save(ctx context.Context, uid string, token []byte) error
	Load(ctx context.Context, uid string) ([]byte, error)
	// Delete はトークンを破棄する。無ければ何もしない。
	Delete(ctx context.Context, uid string) error
}

// IDTokenValidator は ID トークンを検証する（本番は idtoken.Validate）。
type IDTokenValidator func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error)

// Config は OAuth クライアントの設定。
type Config struct {
	ClientID     string
	ClientSecret string
	// AppURL は管理画面のオリジン。同意画面から戻る先は AppURL + /api/calendar/callback。
	AppURL string
	// 以下はテスト用。空なら Google の本番のエンドポイントを使う。
	OAuthEndpoint    oauth2.Endpoint
	CalendarEndpoint string
	HTTPClient       *http.Client
}

// Google は calendar.Google の実装。
type Google struct {
	oauth    oauth2.Config
	tokens   TokenStore
	validate IDTokenValidator
	endpoint string
	base     *http.Client
}

var _ calendar.Google = (*Google)(nil)

// New は Google を作る。validate が nil なら idtoken.Validate を使う。
func New(cfg Config, tokens TokenStore, validate IDTokenValidator) *Google {
	endpoint := cfg.OAuthEndpoint
	if endpoint.TokenURL == "" {
		endpoint = google.Endpoint
	}
	if validate == nil {
		validate = idtoken.Validate
	}
	return &Google{
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: endpoint,
			RedirectURL: strings.TrimRight(cfg.AppURL, "/") + "/api/calendar/callback",
			// events.owned.readonly は、主カレンダーの予定を DOPAGAKI のカレンダー画面に表示するためだけに使う。
			Scopes: []string{"openid", "email", calendarapi.CalendarAppCreatedScope, calendarapi.CalendarEventsOwnedReadonlyScope},
		},
		tokens: tokens, validate: validate, endpoint: cfg.CalendarEndpoint, base: cfg.HTTPClient,
	}
}

func (g *Google) withBase(ctx context.Context) context.Context {
	if g.base == nil {
		return context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 25 * time.Second})
	}
	return context.WithValue(ctx, oauth2.HTTPClient, g.base)
}

// AuthURL は同意画面の URL。
func (g *Google) AuthURL(state, loginHint string) string {
	return g.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent select_account"), oauth2.SetAuthURLParam("login_hint", loginHint))
}

// Exchange は認可コードをトークンに換え、ID トークンの audience と subject を確かめる。
func (g *Google) Exchange(ctx context.Context, code string) (calendar.Grant, error) {
	token, err := g.oauth.Exchange(g.withBase(ctx), code)
	if err != nil {
		return calendar.Grant{}, apperr.External("external.failed", fmt.Errorf("oauth exchange: %s", oauthStatus(err)))
	}
	rawID, _ := token.Extra("id_token").(string)
	if rawID == "" {
		return calendar.Grant{}, apperr.Forbidden("calendar.reauth_required")
	}
	payload, err := g.validate(ctx, rawID, g.oauth.ClientID)
	if err != nil || payload.Subject == "" {
		return calendar.Grant{}, apperr.Forbidden("calendar.reauth_required")
	}
	raw, err := json.Marshal(token) //nolint:gosec // トークンは Secret Manager に保存するために直列化する（ログには出さない）。
	if err != nil {
		return calendar.Grant{}, apperr.Internal(fmt.Errorf("gcal: encode token: %w", err))
	}
	return calendar.Grant{Subject: payload.Subject, HasRefreshToken: token.RefreshToken != "", PrimaryEvents: granted(token, calendarapi.CalendarEventsOwnedReadonlyScope), Token: raw}, nil
}

// granted は、トークンの応答の scope（空白区切り）に scope が含まれるか。同意画面で本人が一部のスコープを外すことがある。
func granted(token *oauth2.Token, scope string) bool {
	scopes, _ := token.Extra("scope").(string)
	return slices.Contains(strings.Fields(scopes), scope)
}

// oauthStatus は OAuth の失敗の HTTP ステータスだけを返す（本文を含めない）。
func oauthStatus(err error) string {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.Response != nil {
		return fmt.Sprintf("status %d", re.Response.StatusCode)
	}
	return fmt.Sprintf("%T", err)
}

// SaveToken は利用者のトークンを保存する。
func (g *Google) SaveToken(ctx context.Context, uid string, token []byte) error {
	return g.tokens.Save(ctx, uid, token)
}

// DeleteToken は利用者のトークンを破棄する。
func (g *Google) DeleteToken(ctx context.Context, uid string) error {
	return g.tokens.Delete(ctx, uid)
}

// Client は保存したトークンで Calendar API を使う。
func (g *Google) Client(ctx context.Context, uid string) (calendar.Client, error) {
	raw, err := g.tokens.Load(ctx, uid)
	if err != nil {
		return nil, err
	}
	return g.ClientFor(ctx, raw)
}

// ClientFor は受け取ったトークンで Calendar API を使う。アクセストークンが切れていれば、リフレッシュトークンで更新する。
func (g *Google) ClientFor(ctx context.Context, raw []byte) (calendar.Client, error) {
	var token oauth2.Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, apperr.Forbidden("calendar.reauth_required")
	}
	httpClient := g.oauth.Client(g.withBase(context.WithoutCancel(ctx)), &token)
	httpClient.Timeout = 25 * time.Second
	opts := []option.ClientOption{option.WithHTTPClient(httpClient)}
	if g.endpoint != "" {
		opts = append(opts, option.WithEndpoint(g.endpoint))
	}
	svc, err := calendarapi.NewService(ctx, opts...)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("gcal: calendar client: %w", err))
	}
	return &client{svc: svc}, nil
}

type client struct {
	svc *calendarapi.Service
}

// mapError は Google の失敗を、calendar が処理を分けるエラーか external.failed にする。本文は含めない。
func mapError(op string, err error) error {
	var refresh *oauth2.RetrieveError
	if errors.As(err, &refresh) && refresh.ErrorCode == "invalid_grant" {
		return apperr.Forbidden("calendar.reauth_required")
	}
	var ge *googleapi.Error
	if !errors.As(err, &ge) {
		return apperr.External("external.failed", fmt.Errorf("google calendar %s: %T", op, err))
	}
	switch ge.Code {
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("google calendar %s: %w", op, calendar.ErrEventGone)
	case http.StatusConflict:
		return fmt.Errorf("google calendar %s: %w", op, calendar.ErrEventExists)
	case http.StatusPreconditionFailed:
		return fmt.Errorf("google calendar %s: %w", op, calendar.ErrPreconditionFailed)
	case http.StatusUnauthorized:
		return apperr.Forbidden("calendar.reauth_required").Wrap(fmt.Errorf("google calendar %s: status %d", op, ge.Code))
	default:
		return apperr.External("external.failed", fmt.Errorf("google calendar %s: status %d", op, ge.Code))
	}
}

func toAPITime(t calendar.EventTime) *calendarapi.EventDateTime {
	return &calendarapi.EventDateTime{DateTime: t.DateTime, Date: t.Date, TimeZone: t.TimeZone}
}

func fromAPITime(t *calendarapi.EventDateTime) calendar.EventTime {
	if t == nil {
		return calendar.EventTime{}
	}
	return calendar.EventTime{DateTime: t.DateTime, Date: t.Date, TimeZone: t.TimeZone}
}

func toAPI(e calendar.Event) *calendarapi.Event {
	out := &calendarapi.Event{
		Id: e.ID, Status: e.Status, Summary: e.Summary, Description: e.Description,
		Start: toAPITime(e.Start), End: toAPITime(e.End), Recurrence: e.Recurrence,
	}
	if e.Transparent {
		out.Transparency = "transparent"
	}
	if len(e.Private) > 0 {
		out.ExtendedProperties = &calendarapi.EventExtendedProperties{Private: e.Private}
	}
	return out
}

func fromAPI(e *calendarapi.Event) calendar.Event {
	out := calendar.Event{
		ID: e.Id, ETag: e.Etag, Status: e.Status, Summary: e.Summary, Description: e.Description,
		Start: fromAPITime(e.Start), End: fromAPITime(e.End), Recurrence: e.Recurrence,
		RecurringEventID: e.RecurringEventId, Transparent: e.Transparency == "transparent",
	}
	if e.ExtendedProperties != nil {
		out.Private = e.ExtendedProperties.Private
	}
	return out
}

func (c *client) CreateCalendar(ctx context.Context, summary, description, timeZone string) (string, error) {
	created, err := c.svc.Calendars.Insert(&calendarapi.Calendar{Summary: summary, Description: description, TimeZone: timeZone}).Context(ctx).Do()
	if err != nil {
		return "", mapError("create calendar", err)
	}
	return created.Id, nil
}

func (c *client) InsertEvent(ctx context.Context, calendarID string, e calendar.Event) (calendar.Event, error) {
	created, err := c.svc.Events.Insert(calendarID, toAPI(e)).Context(ctx).Do()
	if err != nil {
		return calendar.Event{}, mapError("insert event", err)
	}
	return fromAPI(created), nil
}

func (c *client) GetEvent(ctx context.Context, calendarID, eventID string) (calendar.Event, error) {
	e, err := c.svc.Events.Get(calendarID, eventID).Context(ctx).Do()
	if err != nil {
		return calendar.Event{}, mapError("get event", err)
	}
	if e.Status == calendar.EventCancelled {
		return calendar.Event{}, fmt.Errorf("google calendar get event: %w", calendar.ErrEventGone)
	}
	return fromAPI(e), nil
}

func (c *client) UpdateEvent(ctx context.Context, calendarID string, e calendar.Event) (calendar.Event, error) {
	updated, err := c.svc.Events.Update(calendarID, e.ID, toAPI(e)).Context(ctx).Do()
	if err != nil {
		return calendar.Event{}, mapError("update event", err)
	}
	return fromAPI(updated), nil
}

func (c *client) PatchEvent(ctx context.Context, calendarID, eventID, etag string, p calendar.EventPatch) (calendar.Event, error) {
	patch := &calendarapi.Event{Description: p.Description, ForceSendFields: []string{"Description"}}
	if p.Summary != nil {
		patch.Summary = *p.Summary
		patch.ForceSendFields = append(patch.ForceSendFields, "Summary")
	}
	if p.Start != nil {
		patch.Start = toAPITime(*p.Start)
	}
	if p.End != nil {
		patch.End = toAPITime(*p.End)
	}
	call := c.svc.Events.Patch(calendarID, eventID, patch)
	call.Header().Set("If-Match", etag)
	updated, err := call.Context(ctx).Do()
	if err != nil {
		return calendar.Event{}, mapError("patch event", err)
	}
	return fromAPI(updated), nil
}

func (c *client) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	if err := c.svc.Events.Delete(calendarID, eventID).Context(ctx).Do(); err != nil {
		return mapError("delete event", err)
	}
	return nil
}

// SetDefaultReminders はカレンダーの一覧の項目の既定の通知（ポップアップ）を置き換える。空の一覧も送り、通知を無くす。
func (c *client) SetDefaultReminders(ctx context.Context, calendarID string, minutes []int) error {
	reminders := make([]*calendarapi.EventReminder, 0, len(minutes))
	for _, m := range minutes {
		reminders = append(reminders, &calendarapi.EventReminder{Method: "popup", Minutes: int64(m), ForceSendFields: []string{"Minutes"}})
	}
	entry := &calendarapi.CalendarListEntry{DefaultReminders: reminders, ForceSendFields: []string{"DefaultReminders"}}
	if _, err := c.svc.CalendarList.Patch(calendarID, entry).Context(ctx).Do(); err != nil {
		return mapError("set default reminders", err)
	}
	return nil
}

func (c *client) ListEvents(ctx context.Context, calendarID, syncToken, pageToken string) (calendar.EventPage, error) {
	call := c.svc.Events.List(calendarID).MaxResults(250).ShowDeleted(true)
	if syncToken != "" {
		call = call.SyncToken(syncToken)
	}
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	res, err := call.Context(ctx).Do()
	if err != nil {
		var ge *googleapi.Error
		if errors.As(err, &ge) && ge.Code == http.StatusGone {
			return calendar.EventPage{}, fmt.Errorf("google calendar list events: %w", calendar.ErrSyncTokenExpired)
		}
		return calendar.EventPage{}, mapError("list events", err)
	}
	page := calendar.EventPage{NextPageToken: res.NextPageToken, NextSyncToken: res.NextSyncToken}
	for _, e := range res.Items {
		if e != nil {
			page.Events = append(page.Events, fromAPI(e))
		}
	}
	return page, nil
}

func (c *client) ListRange(ctx context.Context, calendarID string, from, to time.Time) ([]calendar.Event, error) {
	var out []calendar.Event
	call := c.svc.Events.List(calendarID).TimeMin(from.Format(time.RFC3339)).TimeMax(to.Format(time.RFC3339)).
		SingleEvents(true).OrderBy("startTime").MaxResults(2500).
		Fields("nextPageToken", "items(id,status,summary,start,end)")
	err := call.Pages(ctx, func(res *calendarapi.Events) error {
		for _, e := range res.Items {
			if e != nil {
				out = append(out, fromAPI(e))
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapError("list range", err)
	}
	return out, nil
}

func (c *client) Watch(ctx context.Context, calendarID string, req calendar.WatchRequest) (calendar.Watch, error) {
	ch := &calendarapi.Channel{Id: req.ChannelID, Type: "web_hook", Address: req.Address, Token: req.Token, Expiration: req.Expires.UnixMilli()}
	res, err := c.svc.Events.Watch(calendarID, ch).Context(ctx).Do()
	if err != nil {
		return calendar.Watch{}, mapError("watch", err)
	}
	w := calendar.Watch{ResourceID: res.ResourceId}
	if res.Expiration > 0 {
		w.Expires = time.UnixMilli(res.Expiration).UTC()
	}
	return w, nil
}

func (c *client) StopChannel(ctx context.Context, channelID, resourceID string) error {
	if err := c.svc.Channels.Stop(&calendarapi.Channel{Id: channelID, ResourceId: resourceID}).Context(ctx).Do(); err != nil {
		return mapError("stop channel", err)
	}
	return nil
}
