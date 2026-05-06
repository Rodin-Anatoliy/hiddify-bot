package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Telegram TelegramConfig
	Hiddify  HiddifyConfig
	DB       DBConfig
	Log      LogConfig
}

type TelegramConfig struct {
	Token   string
	AdminID int64
	Timeout int // long-polling timeout in seconds
}

type HiddifyConfig struct {
	BaseURL    string
	AdminProxy string
	APIKey     string
}

type DBConfig struct {
	Path string
}

type LogConfig struct {
	Level string
}

// MustLoad reads .env (if present) then loads config from environment variables.
// Panics on missing required values — intended to be called once at startup.
func MustLoad() *Config {
	loadDotEnv(".env")

	cfg := &Config{
		Telegram: TelegramConfig{
			Token:   getenv("TG_TOKEN", ""),
			AdminID: getenvInt64("TG_ADMIN_ID", 0),
			Timeout: getenvInt("TG_TIMEOUT", 10),
		},
		Hiddify: HiddifyConfig{
			BaseURL:    getenv("HIDDIFY_BASE_URL", ""),
			AdminProxy: getenv("HIDDIFY_ADMIN_PROXY", ""),
			APIKey:     getenv("HIDDIFY_API_KEY", ""),
		},
		DB: DBConfig{
			Path: getenv("DB_PATH", "data/bot.db"),
		},
		Log: LogConfig{
			Level: getenv("LOG_LEVEL", "info"),
		},
	}

	if err := cfg.validate(); err != nil {
		panic("config: " + err.Error())
	}
	return cfg
}

func (cfg *Config) validate() error {
	var missing []string
	if cfg.Telegram.Token == "" {
		missing = append(missing, "TG_TOKEN")
	}
	if cfg.Telegram.AdminID == 0 {
		missing = append(missing, "TG_ADMIN_ID")
	}
	if cfg.Hiddify.BaseURL == "" {
		missing = append(missing, "HIDDIFY_BASE_URL")
	}
	if cfg.Hiddify.AdminProxy == "" {
		missing = append(missing, "HIDDIFY_ADMIN_PROXY")
	}
	if cfg.Hiddify.APIKey == "" {
		missing = append(missing, "HIDDIFY_API_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvInt64(key string, fallback int64) int64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

// loadDotEnv reads a .env file and sets variables that are not already set.
// Does nothing if the file doesn't exist — safe to call in any environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		// Never override variables already set in the environment.
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
