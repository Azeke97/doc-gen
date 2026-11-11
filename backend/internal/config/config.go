package config

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"
)

// HTTPConfig содержит конфигурацию HTTP-сервера
type HTTPConfig struct {
	Addr              string        `env:"ADDR" envDefault:":8080"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" envDefault:"15s"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout       time.Duration `env:"IDLE_TIMEOUT" envDefault:"60s"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

// DBConfig содержит настройки подключения к базе данных.
type DBConfig struct {
	URL      string `env:"URL,required"`
	MaxConns int32  `env:"MAX_CONNS" envDefault:"10"`
}

// S3Config содержит настройки S3-совместимого хранилища файлов (MinIO).
type S3Config struct {
	Endpoint        string `env:"ENDPOINT,required"`
	AccessKey       string `env:"ACCESS_KEY,required"`
	SecretKey       string `env:"SECRET_KEY,required"`
	UseSSL          bool   `env:"USE_SSL" envDefault:"false"`
	Region          string `env:"REGION" envDefault:"us-east-1"`
	TemplatesBucket string `env:"TEMPLATES_BUCKET" envDefault:"docgen-templates"`
	DocumentsBucket string `env:"DOCUMENTS_BUCKET" envDefault:"docgen-documents"`
}

// Config содержит конфигурацию приложения
type Config struct {
	HTTP     HTTPConfig `envPrefix:"HTTP_"`
	DB       DBConfig   `envPrefix:"DB_"`
	S3       S3Config   `envPrefix:"S3_"`
	LogLevel slog.Level `env:"LOG_LEVEL" envDefault:"info"`
}

// Load читает конфигурацию из переменных окружения
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// LoadDB читает только настройки БД. Нужен утилитам (migrate, keygen),
// которым не нужны HTTP и S3 — им не придётся задавать лишние переменные.
func LoadDB() (DBConfig, error) {
	cfg, err := env.ParseAsWithOptions[DBConfig](env.Options{Prefix: "DB_"})
	if err != nil {
		return DBConfig{}, fmt.Errorf("parse db config: %w", err)
	}
	return cfg, nil
}
