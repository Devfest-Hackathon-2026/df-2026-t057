package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	gfirestore "cloud.google.com/go/firestore"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/cloudtasks"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/firebaseauth"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/firestore"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/gcal"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/gemini"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/googleoidc"
	apihttp "github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/memory"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/aiquota"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/assistant"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/calendar"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/clock"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/id"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/idempotency"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/outbox"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/store"
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

type app struct {
	handler http.Handler
	hub     *realtime.Hub
	closers []func()
}

func (a *app) close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
}

// storage は保存先の実装。
type storage interface {
	store.Backend
	realtime.Watcher
}

// calendarHooks は課題・睡眠・本人の情報の変更をカレンダーへ反映する先。
type calendarHooks interface {
	task.Calendar
	profile.CalendarSync
	sleep.CalendarSync
	Info(ctx context.Context, tx *store.Tx, uid string) (bool, *time.Time, map[string]bool, error)
}

// calendarInfo は相談の get_calendar に連携の状態を渡す。
type calendarInfo struct{ hooks calendarHooks }

func (c calendarInfo) Status(ctx context.Context, tx *store.Tx, uid string) (assistant.CalendarInfo, error) {
	connected, lastSync, linked, err := c.hooks.Info(ctx, tx, uid)
	info := assistant.CalendarInfo{Connected: connected, Sync: "disconnected", LastSync: lastSync, Linked: linked}
	if connected {
		info.Sync = "synced"
	}
	return info, err
}

// build は設定から依存を組み立てる。
func build(ctx context.Context, cfg config.Config, log *slog.Logger) (*app, error) {
	a := &app{}
	clk := clock.System{}
	ids := id.Random{}

	var backend storage
	var verifier apihttp.TokenVerifier
	if cfg.Mode == config.ModeLive {
		client, err := gfirestore.NewClient(ctx, cfg.Project)
		if err != nil {
			return nil, fmt.Errorf("firestore: %w", err)
		}
		a.closers = append(a.closers, func() { _ = client.Close() })
		backend = firestore.New(client)
		if verifier, err = firebaseauth.New(ctx, cfg.Project); err != nil {
			return nil, err
		}
	} else {
		backend = memory.NewStore()
	}
	runner := store.NewRunner(backend)
	idem := idempotency.New(runner, clk, ids)

	// カレンダー連携は live で GOOGLE_OAUTH_CLIENT_ID があるときだけ。ほかは何もしない実装を使う。
	var dispatcher outbox.Dispatcher = memory.Dispatcher{}
	if cfg.CalendarEnabled() {
		d, err := cloudtasks.New(ctx, cfg.Calendar.Queue, cfg.AppURL, cfg.Calendar.TasksInvoker, cfg.OIDCAudience)
		if err != nil {
			return nil, err
		}
		a.closers = append(a.closers, func() { _ = d.Close() })
		dispatcher = d
	}
	jobs := outbox.New(runner, dispatcher, clk, ids, log)
	var hooks calendarHooks = calendar.Disabled{}
	if cfg.CalendarEnabled() {
		hooks = calendar.NewEnqueuer(jobs)
	}
	var planner task.Planner = memory.NoAI{}
	// AI は live だけ。デモは外部に接続せず、決まった計画とクライアントの決まった文を使う。
	var generator companion.Generator
	var model assistant.Model
	if cfg.AIEnabled() {
		quota := aiquota.New(runner, clk, aiquota.Limits{UserPerMinute: cfg.AI.UserPerMinute, UserPerDay: cfg.AI.UserPerDay, GlobalPerMinute: cfg.AI.GlobalPerMinute, GlobalPerDay: cfg.AI.GlobalPerDay})
		client, err := gemini.New(ctx, gemini.Config{Project: cfg.Project, Location: cfg.Location, Model: cfg.GeminiModel}, quota)
		if err != nil {
			return nil, err
		}
		planner, generator, model = client, client, client
	}

	profiles := profile.New(runner, clk, hooks)
	nights := sleep.NewNights(profiles)
	sessions := worksession.Sessions{}
	wallets := wallet.New(runner, clk, profiles, nights, sessions)
	sleeps := sleep.New(runner, clk, profiles, wallets, hooks)
	tasks := task.New(runner, clk, ids, task.Deps{Planner: planner, Calendar: hooks, Sessions: sessions, Profiles: profiles, Rewards: wallets})
	work := worksession.New(runner, clk, ids, tasks, wallets)
	views := viewing.New(runner, clk, wallets, nights)
	recs := records.New(clk, profiles, tasks, wallets, views)
	lines := companion.New(runner, clk, companion.Deps{Tasks: tasks, Profiles: profiles, Wallet: wallets, Sleep: sleeps, Generator: generator})
	companionLines := apihttp.CompanionLines{Service: lines}
	shortcuts := shortcut.New(runner, clk, tasks, profiles, companionLines, cfg.AppURL, log)
	// 相棒の一言は YouTube を開いた回数を参考にし、ショートカットは相棒の一言を使う。互いに使うので、作った後に結ぶ。
	lines.AppOpens = shortcuts
	consult := assistant.New(runner, clk, ids, assistant.Deps{Tasks: tasks, Profiles: profiles, Wallet: wallets, Sleep: sleeps, Sessions: sessions, AppOpens: shortcuts, Calendar: calendarInfo{hooks}, Model: model})

	deps := apihttp.Deps{}
	if cfg.CalendarEnabled() {
		tokens, err := gcal.NewSecretStore(ctx, cfg.Project)
		if err != nil {
			return nil, err
		}
		a.closers = append(a.closers, func() { _ = tokens.Close() })
		google := gcal.New(gcal.Config{ClientID: cfg.Calendar.ClientID, ClientSecret: cfg.Calendar.ClientSecret, AppURL: cfg.AppURL}, tokens, nil)
		svc := calendar.NewService(runner, clk, ids, jobs, calendar.Config{Enabled: true, AppURL: cfg.AppURL}, calendar.Deps{Tasks: tasks, Sleeps: sleeps, Profiles: profiles, Planner: planner, Google: google, Log: log})
		svc.Register(jobs)
		deps.Calendar, deps.Outbox, deps.Internal = apihttp.CalendarLink{Service: svc}, jobs, googleoidc.New(cfg.OIDCAudience)
	}
	a.hub = realtime.NewHub(backend, log)

	deps.Config, deps.Clock, deps.Log, deps.Verifier, deps.Idempotency = cfg, clk, log, verifier, idem
	deps.Profiles, deps.Tasks, deps.Work, deps.Wallet, deps.Viewing, deps.Sleep = profiles, tasks, work, wallets, views, sleeps
	deps.Records, deps.Shortcut, deps.Events = recs, shortcuts, a.hub
	deps.Companion, deps.Assistant = companionLines, apihttp.AssistantTurns{Service: consult}
	api, err := apihttp.NewAPI(deps)
	if err != nil {
		return nil, err
	}
	if cfg.APIOnly {
		a.handler = apihttp.APIOnly(api)
		return a, nil
	}
	var webOpts []apihttp.WebOption
	if cfg.Mode == config.ModeDemo {
		webOpts = append(webOpts, apihttp.WithDemoSession())
	}
	if a.handler, err = apihttp.WithWeb(api, cfg.StaticDir, cfg.AuthDomain, webOpts...); err != nil {
		return nil, err
	}
	return a, nil
}
