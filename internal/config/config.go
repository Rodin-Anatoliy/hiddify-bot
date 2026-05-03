package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?}`)

type Config struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Hiddify  HiddifyConfig  `yaml:"hiddify"`
	DB       DBConfig       `yaml:"db"`
	Log      LogConfig      `yaml:"log"`
}

type TelegramConfig struct {
	Token   string `yaml:"token"`
	AdminID int64  `yaml:"admin_id"`
	Timeout int    `yaml:"timeout"`
}

type HiddifyConfig struct {
	BaseURL    string `yaml:"base_url"`
	AdminProxy string `yaml:"admin_proxy"`
	APIKey     string `yaml:"api_key"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

func MustLoad(path string) *Config {
	loadDotEnv(".env")
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		panic("config: read " + path + ": " + err.Error())
	}

	expanded := expandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		panic("config: " + err.Error())
	}
	if err := cfg.Validate(); err != nil {
		panic("config: " + err.Error())
	}
	return cfg
}

func (cfg *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(cfg.Telegram.Token) == "" {
		missing = append(missing, "telegram.token")
	}
	if cfg.Telegram.AdminID == 0 {
		missing = append(missing, "telegram.admin_id")
	}
	if strings.TrimSpace(cfg.Hiddify.BaseURL) == "" {
		missing = append(missing, "hiddify.base_url")
	}
	if strings.TrimSpace(cfg.Hiddify.AdminProxy) == "" {
		missing = append(missing, "hiddify.admin_proxy")
	}
	if strings.TrimSpace(cfg.Hiddify.APIKey) == "" {
		missing = append(missing, "hiddify.api_key")
	}
	if strings.TrimSpace(cfg.DB.Path) == "" {
		cfg.DB.Path = "data/bot.db"
	}
	if strings.TrimSpace(cfg.Log.Level) == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Telegram.Timeout <= 0 {
		cfg.Telegram.Timeout = 10 // default polling timeout in seconds
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
	}
	return nil
}

func expandEnv(value string) string {
	return envPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := envPattern.FindStringSubmatch(match)
		if len(parts) == 0 {
			return match
		}
		if envValue, ok := os.LookupEnv(parts[1]); ok {
			return envValue
		}
		if len(parts) >= 4 {
			return parts[3]
		}
		return ""
	})
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
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
		if key != "" {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
			_ = os.Setenv(key, value)
		}
	}
}
