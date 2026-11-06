package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
func main() {
	ctx := context.Background()
	mode := env("APP_MODE", "live")
	project := env("GOOGLE_CLOUD_PROJECT", "devfest-2026-513816")
	appURL := env("APP_URL", "http://127.0.0.1:4200")
	server := &app.Server{Mode: mode, Project: project, AppURL: appURL, Planner: &app.Planner{Model: env("GEMINI_MODEL", "gemini-3.8-flash")}}
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
	mux := http.NewServeMux()
	mux.Handle("/api/", server.Handler())
	static := env("STATIC_DIR", "../dist/web/browser")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(static, filepath.Clean(r.URL.Path))); err != nil {
			http.ServeFile(w, r, filepath.Join(static, "index.html"))
			return
		}
		http.FileServer(http.Dir(static)).ServeHTTP(w, r)
	})
	address := ":" + env("PORT", "8080")
	if mode == "demo" && os.Getenv("DEMO_PUBLIC") != "true" {
		address = "127.0.0.1" + address
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		mux.ServeHTTP(w, r)
	})
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
func fatal(err error) { slog.Error("startup failed", "error", err); os.Exit(1) }
