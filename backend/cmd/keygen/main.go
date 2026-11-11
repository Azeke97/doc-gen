package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/apikey"
	"docgen/internal/config"
	"docgen/internal/db"
)

func main() {
	name := flag.String("name", "", "имя клиента для людей")
	project := flag.String("project", "", "код проекта, к шаблонам которого ключ дает доступ")
	flag.Parse()

	if *name == "" || *project == "" {
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.LoadDB()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.URL)
	if err != nil {
		slog.Error("failed to create database pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	key := apikey.Generate()
	client, err := db.New(pool).CreateAPIClient(ctx, db.CreateAPIClientParams{
		Name:      *name,
		Project:   *project,
		KeyHash:   apikey.Hash(key),
		KeyPrefix: apikey.DisplayPrefix(key),
	})
	if err != nil {
		slog.Error("failed to create api client", "err", err)
		os.Exit(1)
	}

	fmt.Printf(
		"API key for %q (project: %s, id: %s):\n\n %s\n\nSave it now - it will not be shown again\n",
		client.Name,
		client.Project,
		client.ID,
		key,
	)
}
