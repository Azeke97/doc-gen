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

// Config содержит конфигурацию приложения
type Config struct {
	HTTP     HTTPConfig `envPrefix:"HTTP_"`
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
