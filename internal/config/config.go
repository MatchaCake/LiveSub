package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Google   GoogleConfig   `yaml:"google"`
	Gemini   GeminiConfig   `yaml:"gemini"`
	Bilibili BilibiliConfig `yaml:"bilibili"`
	Streams  []StreamConfig `yaml:"streams"`
	WebPort  int            `yaml:"web_port"`  // control panel port (default 8899)
	Auth     AuthConfig     `yaml:"auth"`      // web UI authentication
}

type AuthConfig struct {
	Username string `yaml:"username"` // login username
	Password string `yaml:"password"` // login password
}

type GoogleConfig struct {
	Credentials string   `yaml:"credentials"`   // path to service account JSON
	STTLanguage string   `yaml:"stt_language"`   // primary language
	AltLangs    []string `yaml:"alt_langs"`      // additional languages for auto-detection
}

type GeminiConfig struct {
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`       // e.g. "gemini-3.0-flash"
	TargetLang string `yaml:"target_lang"` // translate to
}

type BilibiliConfig struct {
	SESSDATA     string            `yaml:"sessdata"`
	BiliJCT      string            `yaml:"bili_jct"`      // csrf token
	UID          int64             `yaml:"uid"`            // user id
	DanmakuMax   int               `yaml:"danmaku_max"`   // max chars per danmaku (default 20, UL20+=30)
	Accounts     []BilibiliAccount `yaml:"accounts"`       // multiple accounts for failover
}

type BilibiliAccount struct {
	Name       string `yaml:"name"`         // display name
	SESSDATA   string `yaml:"sessdata"`
	BiliJCT    string `yaml:"bili_jct"`
	UID        int64  `yaml:"uid"`
	DanmakuMax int    `yaml:"danmaku_max"`  // per-account max chars (0=use global default)
}

type StreamConfig struct {
	Name       string   `yaml:"name"`
	RoomID     int64    `yaml:"room_id"`
	SourceLang string   `yaml:"source_lang"`   // primary language (optional, default auto-detect)
	AltLangs   []string `yaml:"alt_langs"`     // additional languages e.g. ["en-US", "zh-CN"]
	TargetLang string   `yaml:"target_lang"`   // override per-stream
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Gemini: GeminiConfig{
			Model:      "gemini-3-flash-preview",
			TargetLang: "zh-CN",
		},
		Google: GoogleConfig{
			STTLanguage: "ja-JP",
			AltLangs:    []string{"en-US"},
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Resolve credentials path relative to config file directory
	if cfg.Google.Credentials != "" && !filepath.IsAbs(cfg.Google.Credentials) {
		configDir := filepath.Dir(path)
		cfg.Google.Credentials = filepath.Join(configDir, cfg.Google.Credentials)
	}

	// Set GOOGLE_APPLICATION_CREDENTIALS if not already set
	if cfg.Google.Credentials != "" && os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.Google.Credentials)
	}

	// Fill defaults for streams
	for i := range cfg.Streams {
		if cfg.Streams[i].SourceLang == "" {
			cfg.Streams[i].SourceLang = cfg.Google.STTLanguage
		}
		if len(cfg.Streams[i].AltLangs) == 0 {
			cfg.Streams[i].AltLangs = cfg.Google.AltLangs
		}
		if cfg.Streams[i].TargetLang == "" {
			cfg.Streams[i].TargetLang = cfg.Gemini.TargetLang
		}
	}

	return cfg, nil
}
