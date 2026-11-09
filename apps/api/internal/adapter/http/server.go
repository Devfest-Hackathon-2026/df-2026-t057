package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/account"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/idempotency"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/profile"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/realtime"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/records"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/shortcut"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/sleep"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/viewing"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/wallet"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/worksession"
)

// Companion は相棒の一言。AI が使えなければ空の一言と generated=false を返す。
type Companion interface {
	Line(ctx context.Context, uid, moment, taskID string) (line string, generated bool, err error)
}

// Assistant は AI 相談。
type Assistant interface {
	List(ctx context.Context, uid string) ([]oapi.AssistantTurn, error)
	Send(ctx context.Context, uid, id, text, taskID string) (oapi.AssistantTurn, error)
}

// Calendar は Google カレンダーとの連携。
type Calendar interface {
	Status(ctx context.Context, uid string) (oapi.CalendarStatus, error)
	Connect(ctx context.Context, uid, googleSubject, email string) (string, error)
	Callback(ctx context.Context, state, code, errParam string) string
	SyncNow(ctx context.Context, uid string) (oapi.CalendarStatus, error)
	Disconnect(ctx context.Context, uid string) (oapi.CalendarStatus, error)
	SetReminders(ctx context.Context, uid string, minutes []int) (oapi.CalendarStatus, error)
	Webhook(ctx context.Context, channelID, token, resourceState string) error
	Renew(ctx context.Context) error
}

// OutboxRunner は Cloud Tasks から呼ばれたジョブを実行する。
type OutboxRunner interface {
	Run(ctx context.Context, uid, jobID string) error
}

// InternalAuth は Cloud Tasks・Cloud Scheduler の OIDC トークンを確かめる。
type InternalAuth interface {
	Verify(ctx context.Context, authorization, expectedEmail string) error
}

// Deps はサーバーが使う機能。
type Deps struct {
	Accounts    *account.Service
	Config      config.Config
	Clock       clock.Clock
	Log         *slog.Logger
	Verifier    TokenVerifier
	Idempotency *idempotency.Service
	Profiles    *profile.Service
	Tasks       *task.Service
	Work        *worksession.Service
	Wallet      *wallet.Service
	Viewing     *viewing.Service
	Sleep       *sleep.Service
	Records     *records.Service
	Shortcut    *shortcut.Service
	Companion   Companion
	Assistant   Assistant
	Calendar    Calendar
	Outbox      OutboxRunner
	Internal    InternalAuth
	Events      *realtime.Hub
}

// Server は strict-server の実装。
type Server struct {
	Deps
}

var _ oapi.StrictServerInterface = (*Server)(nil)

type rawAuthKey struct{}

// NewAPI は /api/ の要求を受ける http.Handler を作る。
func NewAPI(d Deps) (http.Handler, error) {
	if d.Calendar == nil {
		d.Calendar = calendarOff{appURL: d.Config.AppURL}
	}
	if d.Companion == nil {
		d.Companion = companionOff{}
	}
	if d.Assistant == nil {
		d.Assistant = assistantOff{}
	}
	rs, err := loadRoutes()
	if err != nil {
		return nil, err
	}
	s := &Server{Deps: d}
	strict := oapi.NewStrictHandlerWithOptions(s, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, r, d.Log, apperr.Invalid("request.malformed").Wrap(err))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { writeProblem(w, r, d.Log, err) },
	})
	mux := http.NewServeMux()
	generated := oapi.HandlerWithOptions(strict, oapi.StdHTTPServerOptions{
		BaseURL:    "/api",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			var missing *oapi.RequiredHeaderError
			if errors.As(err, &missing) && missing.ParamName == "If-Match" {
				writeProblem(w, r, d.Log, apperr.PreconditionRequired("precondition.required"))
				return
			}
			writeProblem(w, r, d.Log, apperr.Invalid("request.invalid").Wrap(err))
		},
	})
	events := eventsHandler(d.Events)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), rawAuthKey{}, r.Header.Get("Authorization"))
		if r.URL.Path == "/api/events" {
			events.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
		defer cancel()
		generated.ServeHTTP(w, r.WithContext(ctx))
	})
	var accountOpen func(context.Context, string) error
	if d.Accounts != nil {
		accountOpen = d.Accounts.CheckOpen
	}
	extensions := origins{appURL: d.Config.AppURL, extensionIDs: d.Config.ExtensionIDs, anyExtension: d.Config.Mode == config.ModeDemo}
	return observe(d.Log, rs, apiPipeline{
		log: d.Log, routes: rs, origins: extensions, idempotency: d.Idempotency, next: inner, accountOpen: accountOpen,
		auth: authenticator{demo: d.Config.Mode == config.ModeDemo, verifier: d.Verifier},
	}), nil
}

func rawAuthorization(ctx context.Context) string {
	v, _ := ctx.Value(rawAuthKey{}).(string)
	return v
}

// calendarOff はカレンダー連携を使わない環境（デモなど）。
type calendarOff struct{ appURL string }

func (calendarOff) Status(context.Context, string) (oapi.CalendarStatus, error) {
	return oapi.CalendarStatus{Enabled: false, Connected: false, Sync: oapi.CalendarStatusSyncDisconnected}, nil
}
func (calendarOff) Connect(context.Context, string, string, string) (string, error) {
	return "", apperr.Unavailable("calendar.unavailable")
}
func (c calendarOff) Callback(context.Context, string, string, string) string {
	return c.appURL + "/?calendar=error"
}
func (calendarOff) SyncNow(context.Context, string) (oapi.CalendarStatus, error) {
	return oapi.CalendarStatus{}, apperr.Unavailable("calendar.unavailable")
}
func (calendarOff) Disconnect(context.Context, string) (oapi.CalendarStatus, error) {
	return oapi.CalendarStatus{}, apperr.Unavailable("calendar.unavailable")
}
func (calendarOff) SetReminders(context.Context, string, []int) (oapi.CalendarStatus, error) {
	return oapi.CalendarStatus{}, apperr.Unavailable("calendar.unavailable")
}
func (calendarOff) Webhook(context.Context, string, string, string) error {
	return apperr.Forbidden("not_found")
}
func (calendarOff) Renew(context.Context) error { return apperr.NotFound("not_found") }

// companionOff は AI を使わない環境の相棒の一言。クライアントが決まった文を出す。
type companionOff struct{}

func (companionOff) Line(context.Context, string, string, string) (string, bool, error) {
	return "", false, nil
}

// assistantOff は AI を使わない環境の相談。
type assistantOff struct{}

func (assistantOff) List(context.Context, string) ([]oapi.AssistantTurn, error) {
	return []oapi.AssistantTurn{}, nil
}

func (assistantOff) Send(context.Context, string, string, string, string) (oapi.AssistantTurn, error) {
	return oapi.AssistantTurn{}, apperr.Unavailable("ai.unavailable")
}
