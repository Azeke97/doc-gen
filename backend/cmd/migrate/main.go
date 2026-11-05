package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"docgen/internal/config"
	"docgen/migrations"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down|status|redo|version>")
		os.Exit(2)
	}

	command := os.Args[1]

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("pgx", cfg.DB.URL)
	if err != nil {
		slog.Error("failed to open database connection", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		slog.Error("failed to set goose dialect", "err", err)
		os.Exit(1)
	}

	if err := goose.RunContext(ctx, command, db, "."); err != nil {
		slog.Error("migration failed", "command", command, "err", err)
		os.Exit(1)
	}
}
