package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/config"
	"docgen/internal/db"
	"docgen/internal/httpapi"
	"docgen/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCfg, err := pgxpool.ParseConfig(cfg.DB.URL)
	if err != nil {
		slog.Error("failed to parse database config", "err", err)
		os.Exit(1)
	}
	poolCfg.MaxConns = cfg.DB.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		slog.Error("failed to create database pool", "err", err)
		os.Exit(1)
	}

	defer pool.Close()

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := pool.Ping(pingCtx); err != nil {
		slog.Error("failed to ping database", "err", err)
		os.Exit(1)
	}

	slog.Info("database connected")

	store, err := storage.New(storage.Config{
		Endpoint:  cfg.S3.Endpoint,
		AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey,
		UseSSL:    cfg.S3.UseSSL,
		Region:    cfg.S3.Region,
	})
	if err != nil {
		slog.Error("failed to create storage", "err", err)
		os.Exit(1)
	}

	bucketCtx, bucketCancel := context.WithTimeout(ctx, 5*time.Second)
	defer bucketCancel()
	for _, bucket := range []string{cfg.S3.TemplatesBucket, cfg.S3.DocumentsBucket} {
		if err := store.EnsureBucket(bucketCtx, bucket); err != nil {
			slog.Error("failed to prepare storage bucket", "bucket", bucket, "err", err)
			os.Exit(1)
		}
	}

	slog.Info("storage ready", "endpoint", cfg.S3.Endpoint)

	queries := db.New(pool)

	serv := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			Logger:          logger,
			APIClients:      queries,
			AdminAuth:       queries,
			AdminSessionTTL: cfg.Admin.SessionTTL,
			Projects:        queries,
		}),
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)

	go func() {
		slog.InfoContext(ctx, "starting server", "addr", serv.Addr)

		if err := serv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("server failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()
	if err := serv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "err", err)
	} else {
		slog.Info("server stopped")
	}
}
