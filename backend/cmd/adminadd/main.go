package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"docgen/internal/adminauth"
	"docgen/internal/config"
	"docgen/internal/db"
	"docgen/internal/pgerr"
)

func main() {
	email := flag.String("email", "", "email администратора, он же логин")
	name := flag.String("name", "", "имя администратора")
	flag.Parse()

	*email = strings.ToLower(strings.TrimSpace(*email))
	if *email == "" || *name == "" {
		flag.Usage()
		os.Exit(1)
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
		slog.Error("failed to create database connection pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	password := adminauth.GeneratePassword()
	hash, err := adminauth.HashPassword(password)
	if err != nil {
		slog.Error("failed to hash password", "err", err)
		os.Exit(1)
	}

	user, err := db.New(pool).CreateAdminUser(ctx, db.CreateAdminUserParams{
		Email:        *email,
		Name:         *name,
		PasswordHash: hash,
	})
	if pgerr.IsUniqueViolation(err) {
		fmt.Fprintf(os.Stderr, "admin %s already exists\n", *email)
		os.Exit(1)
	}
	if err != nil {
		slog.Error("failed to create admin user", "err", err)
		os.Exit(1)
	}

	fmt.Printf("Admin %s <%s> created (id: %s).\n\nPassword:\n\n  %s\n\nSave it now — it will not be shown again.\n",
		user.Name, user.Email, user.ID, password)
}
