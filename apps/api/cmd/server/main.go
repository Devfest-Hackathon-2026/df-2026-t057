// Command server は DOPAGAKI の API と管理画面を配信する。設定の読み込みと依存の組み立てだけを行う。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // コンテナに tzdata が無くても、本人のタイムゾーンを読み込めるようにする。

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := build(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer app.close()

	host := ""
	if cfg.Mode == config.ModeDemo && !cfg.Public {
		host = "127.0.0.1"
	}
	srv := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(cfg.Port)),
		Handler:           app.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		// SSE は接続ごとに書き込みの期限を外す（adapter/http の eventsHandler）。
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	// 終了するときは、変更通知の長い接続を先に閉じる（Shutdown が待ち続けないように）。
	srv.RegisterOnShutdown(app.hub.Close)
	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "mode", cfg.Mode)
		errs <- srv.ListenAndServe()
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
