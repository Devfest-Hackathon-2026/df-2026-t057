package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	firebase "firebase.google.com/go/v4"
	"github.com/a-sumikawa/devfest-2026/api/internal/app"
	"google.golang.org/genai"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func envLimit(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil || value < 1 {
		slog.Error("invalid AI limit configuration", "variable", key)
		os.Exit(1)
	}
	return value
}
func main() {
	ctx := context.Background()
	mode := env("APP_MODE", "live")
	project := env("GOOGLE_CLOUD_PROJECT", "devfest-2026-513816")
	appURL := env("APP_URL", "http://127.0.0.1:4200")
	server := &app.Server{ExtensionIDs: strings.Fields(os.Getenv("EXTENSION_IDS")), MaintenanceEmail: os.Getenv("MAINTENANCE_EMAIL"), SchedulerSecret: os.Getenv("SCHEDULER_SECRET"), AccountCleanupEnabled: os.Getenv("ACCOUNT_CLEANUP_ENABLED") == "true", Mode: mode, Project: project, AppURL: appURL, Planner: &app.Planner{Model: env("GEMINI_MODEL", "gemini-3.8-flash")}}
	if raw := os.Getenv("FIREBASE_WEB_CONFIG"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &server.Firebase); err != nil {
			slog.Error("invalid Firebase web configuration")
			os.Exit(1)
		}
	}
	if mode == "demo" {
		server.Store = app.NewMemoryStore()
		server.Assistant = app.NewMemoryAssistantStore()
	} else {
		db, err := firestore.NewClient(ctx, project)
		if err != nil {
			fatal(err)
		}
		defer db.Close()
		store := &app.FirestoreStore{DB: db}
		limits := app.DefaultAILimits()
		server.Planner.Limits = app.AILimits{
			UserPerMinute:   envLimit("AI_USER_PER_MINUTE", limits.UserPerMinute),
			UserPerDay:      envLimit("AI_USER_PER_DAY", limits.UserPerDay),
			GlobalPerMinute: envLimit("AI_GLOBAL_PER_MINUTE", limits.GlobalPerMinute),
			GlobalPerDay:    envLimit("AI_GLOBAL_PER_DAY", limits.GlobalPerDay),
		}
		server.Planner.Usage = &app.FirestoreAIUsageStore{DB: db}
		server.Store = store
		server.Assistant = &app.FirestoreAssistantStore{DB: db}
		firebaseApp, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: project})
		if err != nil {
			fatal(err)
		}
		server.Auth, err = firebaseApp.Auth(ctx)
		if err != nil {
			fatal(err)
		}
		server.Planner.Client, err = genai.NewClient(ctx, &genai.ClientConfig{Project: project, Location: env("GOOGLE_CLOUD_LOCATION", "global"), Backend: genai.BackendVertexAI})
		if err != nil {
			fatal(err)
		}
		if os.Getenv("GOOGLE_OAUTH_CLIENT_ID") != "" {
			secrets, err := secretmanager.NewClient(ctx)
			if err != nil {
				fatal(err)
			}
			defer secrets.Close()
			server.Calendar = app.NewCalendar(db, secrets, project, appURL, store, server.Planner)
		}
	}
	if mode != "demo" && server.Calendar == nil {
		server.AccountCleanupEnabled = false
	}
	static := env("STATIC_DIR", "../dist/web/browser")
	var handler http.Handler = server.Handler()
	if mode != "demo" || os.Getenv("DEV_API_ONLY") != "true" {
		var err error
		handler, err = app.WebHandler(handler, static, server.Firebase["authDomain"], strings.Fields(os.Getenv("EXTENSION_IDS"))...)
		if err != nil {
			fatal(err)
		}
	}
	address := ":" + env("PORT", "8080")
	if mode == "demo" && os.Getenv("DEMO_PUBLIC") != "true" {
		address = "127.0.0.1" + address
	}
	httpServer := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()
	slog.Info("DOPAGAKI API ready", "address", address, "mode", mode)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}
func fatal(err error) { app.LogFailure("startup failed", err); os.Exit(1) }
