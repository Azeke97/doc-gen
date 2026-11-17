// Команда keygen управляет API-ключами порталов:
//
//	keygen -name=... -project=...   выпустить ключ (печатается один раз)
//	keygen -list [-project=...]     список ключей: ID, начало ключа, портал, отозван ли
//	keygen -revoke=<ID ключа>       отозвать ключ: портал с ним сразу перестаёт проходить
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/apikey"
	"docgen/internal/config"
	"docgen/internal/db"
	"docgen/internal/pgerr"
)

func main() {
	name := flag.String("name", "", "имя клиента для людей")
	project := flag.String("project", "", "код проекта (портала), от имени которого ключ выпускает документы")
	list := flag.Bool("list", false, "показать ключи (всех проектов или одного -project)")
	revoke := flag.String("revoke", "", "ID ключа, который нужно отозвать (из -list)")
	flag.Parse()

	switch {
	case *list && *revoke != "",
		!*list && *revoke == "" && (*name == "" || *project == ""):
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.LoadDB()
	if err != nil {
		fail("failed to load config", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.URL)
	if err != nil {
		fail("failed to create database pool", err)
	}
	defer pool.Close()
	q := db.New(pool)

	switch {
	case *list:
		listKeys(ctx, q, *project)
	case *revoke != "":
		revokeKey(ctx, q, *revoke)
	default:
		createKey(ctx, q, *name, *project)
	}
}

func createKey(ctx context.Context, q *db.Queries, name, project string) {
	key := apikey.Generate()
	client, err := q.CreateAPIClient(ctx, db.CreateAPIClientParams{
		Name:      name,
		Project:   project,
		KeyHash:   apikey.Hash(key),
		KeyPrefix: apikey.DisplayPrefix(key),
	})
	if pgerr.IsForeignKeyViolation(err) {
		fmt.Fprintf(os.Stderr, "project %q does not exist, create it first\n", project)
		os.Exit(1)
	}
	if err != nil {
		fail("failed to create api client", err)
	}

	fmt.Printf(
		"API key for %q (project: %s, id: %s):\n\n %s\n\nSave it now - it will not be shown again\n",
		client.Name, client.Project, client.ID, key,
	)
}

func listKeys(ctx context.Context, q *db.Queries, project string) {
	var filter *string
	if project != "" {
		filter = &project
	}
	keys, err := q.ListAPIClients(ctx, filter)
	if err != nil {
		fail("failed to list api clients", err)
	}
	if len(keys) == 0 {
		fmt.Println("No API keys.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tKEY\tPROJECT\tNAME\tCREATED\tSTATUS")
	for _, k := range keys {
		status := "active"
		if k.RevokedAt != nil {
			status = "revoked " + k.RevokedAt.Format("2006-01-02 15:04")
		}
		_, _ = fmt.Fprintf(w, "%s\t%s…\t%s\t%s\t%s\t%s\n", k.ID, k.KeyPrefix, k.Project, k.Name, k.CreatedAt.Format("2006-01-02 15:04"), status)
	}
	_ = w.Flush()
}

func revokeKey(ctx context.Context, q *db.Queries, rawID string) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%q is not a key ID: take the ID from keygen -list\n", rawID)
		os.Exit(2)
	}
	k, err := q.RevokeAPIClient(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		fmt.Fprintf(os.Stderr, "no active key with ID %s\n", id)
		os.Exit(1)
	}
	if err != nil {
		fail("failed to revoke api client", err)
	}
	fmt.Printf("Key %s… (%q, project %s) is revoked: requests with it now get 401.\n", k.KeyPrefix, k.Name, k.Project)
}

func fail(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
