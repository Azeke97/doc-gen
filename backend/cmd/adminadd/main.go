// Команда adminadd управляет администраторами:
//
//	adminadd -email=... -name=...   создать администратора (пароль печатается один раз)
//	adminadd -demo                  создать или обновить тестовую запись бета-теста из ADMIN_DEMO_*
//	adminadd -disable -email=...    заблокировать запись и закрыть её сессии
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

const demoName = "Тестовый администратор"

func main() {
	email := flag.String("email", "", "email администратора, он же логин")
	name := flag.String("name", "", "имя администратора")
	demo := flag.Bool("demo", false, "создать или обновить тестовую запись из ADMIN_DEMO_EMAIL и ADMIN_DEMO_PASSWORD")
	disable := flag.Bool("disable", false, "заблокировать администратора с этим email")
	flag.Parse()

	*email = strings.ToLower(strings.TrimSpace(*email))
	switch {
	case *demo && *disable,
		!*demo && *email == "",
		!*demo && !*disable && *name == "":
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

	switch {
	case *demo:
		upsertDemo(ctx, q)
	case *disable:
		disableAdmin(ctx, pool, *email)
	default:
		createAdmin(ctx, q, *email, *name)
	}
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

// upsertDemo создаёт тестовую запись или выставляет ей пароль из настроек заново: так пароль
// в базе всегда совпадает с тем, что показывает страница входа.
func upsertDemo(ctx context.Context, q *db.Queries) {
	cfg, err := config.LoadAdminDemo()
	if err != nil {
		fail("demo account is not configured", err)
	}
	hash, err := adminauth.HashPassword(cfg.DemoPassword)
	if err != nil {
		fail("failed to hash password", err)
	}
	user, err := q.UpsertAdminUser(ctx, db.UpsertAdminUserParams{
		Email: strings.ToLower(strings.TrimSpace(cfg.DemoEmail)), Name: demoName, PasswordHash: hash,
	})
	if err != nil {
		fail("failed to save demo admin", err)
	}
	fmt.Printf("Demo admin %s is ready (id: %s). The login page shows it while ADMIN_DEMO_LOGIN=true.\n", user.Email, user.ID)
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
