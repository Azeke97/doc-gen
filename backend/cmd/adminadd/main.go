// Команда adminadd управляет администраторами:
//
//	adminadd -email=... -name=...   создать администратора (пароль печатается один раз)
//	adminadd -disable -email=...    заблокировать администратора и закрыть его сессии
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/adminauth"
	"docgen/internal/config"
	"docgen/internal/db"
	"docgen/internal/pgerr"
)

func main() {
	email := flag.String("email", "", "email администратора, он же логин")
	name := flag.String("name", "", "имя администратора")
	disable := flag.Bool("disable", false, "заблокировать администратора с этим email")
	flag.Parse()

	*email = strings.ToLower(strings.TrimSpace(*email))
	if *email == "" || (!*disable && *name == "") {
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
		fail("failed to create database connection pool", err)
	}
	defer pool.Close()
	q := db.New(pool)

	if *disable {
		disableAdmin(ctx, pool, *email)
		return
	}
	createAdmin(ctx, q, *email, *name)
}

func createAdmin(ctx context.Context, q *db.Queries, email, name string) {
	password := adminauth.GeneratePassword()
	hash, err := adminauth.HashPassword(password)
	if err != nil {
		fail("failed to hash password", err)
	}

	user, err := q.CreateAdminUser(ctx, db.CreateAdminUserParams{Email: email, Name: name, PasswordHash: hash})
	if pgerr.IsUniqueViolation(err) {
		fmt.Fprintf(os.Stderr, "admin %q already exists\n", email)
		os.Exit(1)
	}
	if err != nil {
		fail("failed to create admin user", err)
	}

	fmt.Printf("Admin %s <%s> created (id: %s).\n\nPassword:\n\n  %s\n\nSave it now — it will not be shown again.\n",
		user.Name, user.Email, user.ID, password)
}

func disableAdmin(ctx context.Context, pool *pgxpool.Pool, email string) {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		id, err := q.DisableAdminUser(ctx, email)
		if err != nil {
			return err
		}
		return q.DeleteAdminUserSessions(ctx, id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fmt.Fprintf(os.Stderr, "no active admin %q\n", email)
		os.Exit(1)
	}
	if err != nil {
		fail("failed to disable admin", err)
	}
	fmt.Printf("Admin %s is disabled and signed out.\n", email)
}

func fail(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
