package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	koanfjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Default configuration values.
const (
	defaultUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	defaultAIModel = "claude-sonnet-4-5-20250929"
)

// aiConfig holds AI solver configuration.
type aiConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Model   string `json:"model,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

// appConfig holds the application configuration.
type appConfig struct {
	BaseURL   string   `json:"base_url"`
	Cookie    string   `json:"cookie"`
	UserAgent string   `json:"user_agent"`
	AI        aiConfig `json:"ai,omitempty"`
}

func defaultConfig() appConfig {
	return appConfig{
		UserAgent: defaultUA,
		AI: aiConfig{
			Enabled: true,
			Model:   defaultAIModel,
		},
	}
}

// loadConfig loads configuration from the specified path.
func loadConfig(path string) (appConfig, error) {
	cfg := defaultConfig()

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return appConfig{}, fmt.Errorf("stat config: %w", err)
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), koanfjson.Parser()); err != nil {
		return appConfig{}, fmt.Errorf("load config: %w", err)
	}
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "json"}); err != nil {
		return appConfig{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Cookie = strings.TrimSpace(cfg.Cookie)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.BaseURL == "" {
		return appConfig{}, errors.New("base_url is required in config")
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUA
	}
	if strings.TrimSpace(cfg.AI.Model) == "" {
		cfg.AI.Model = defaultAIModel
	}
	return cfg, nil
}

// saveConfig writes configuration to the specified path.
func saveConfig(path string, cfg appConfig) error {
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUA
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	b = append(b, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
