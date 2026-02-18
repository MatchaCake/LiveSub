package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Streamer    StreamerConfig    `yaml:"streamer"`
	STT         STTConfig         `yaml:"stt"`
	Translation TranslationConfig `yaml:"translation"`
	Outputs     []OutputConfig    `yaml:"outputs"`
	Bots        []BotConfig       `yaml:"bots"`
	Web         WebConfig         `yaml:"web"`
}

type StreamerConfig struct {
	Name       string   `yaml:"name"`
	RoomID     int64    `yaml:"room_id"`
	SourceLang string   `yaml:"source_lang"` // primary STT language (e.g. "ja-JP")
	AltLangs   []string `yaml:"alt_langs"`   // additional detection languages
}

type STTConfig struct {
	Provider    string `yaml:"provider"`    // "google"
	Credentials string `yaml:"credentials"` // path to service account JSON
}

type TranslationConfig struct {
	Provider string `yaml:"provider"` // "gemini"
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"` // e.g. "gemini-2.0-flash"
}

type OutputConfig struct {
	Name       string `yaml:"name"`
	Platform   string `yaml:"platform"`    // "bilibili"
	TargetLang string `yaml:"target_lang"` // empty = send source text (no translation)
	Account    string `yaml:"account"`     // bot name reference
	RoomID     int64  `yaml:"room_id"`     // 0 = use streamer's room_id
	Prefix     string `yaml:"prefix"`
	Suffix     string `yaml:"suffix"`
}

type BotConfig struct {
	Name       string `yaml:"name"`
	Platform   string `yaml:"platform"` // "bilibili"
	SESSDATA   string `yaml:"sessdata"`
	BiliJCT    string `yaml:"bili_jct"`
	UID        int64  `yaml:"uid"`
	DanmakuMax int    `yaml:"danmaku_max"`
}

type WebConfig struct {
	Port int        `yaml:"port"`
	Auth AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		STT: STTConfig{
			Provider: "google",
		},
		Translation: TranslationConfig{
			Provider: "gemini",
			Model:    "gemini-2.0-flash",
		},
		Streamer: StreamerConfig{
			SourceLang: "ja-JP",
			AltLangs:   []string{"en-US"},
		},
		Web: WebConfig{
			Port: 8899,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Resolve credentials path relative to config file directory
	if cfg.STT.Credentials != "" && !filepath.IsAbs(cfg.STT.Credentials) {
		configDir := filepath.Dir(path)
		cfg.STT.Credentials = filepath.Join(configDir, cfg.STT.Credentials)
	}

	// Set GOOGLE_APPLICATION_CREDENTIALS if not already set
	if cfg.STT.Credentials != "" && os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.STT.Credentials)
	}

	// Default output platform
	for i := range cfg.Outputs {
		if cfg.Outputs[i].Platform == "" {
			cfg.Outputs[i].Platform = "bilibili"
		}
	}

	// Default bot settings
	for i := range cfg.Bots {
		if cfg.Bots[i].Platform == "" {
			cfg.Bots[i].Platform = "bilibili"
		}
		if cfg.Bots[i].DanmakuMax <= 0 {
			cfg.Bots[i].DanmakuMax = 20
		}
	}

	return cfg, nil
}

// Save writes the config back to the given path.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
