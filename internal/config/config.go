package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Streamers   []StreamerConfig  `yaml:"streamers" json:"streamers"`
	STT         STTConfig         `yaml:"stt" json:"stt"`
	Translation TranslationConfig `yaml:"translation" json:"translation"`
	Bots        []BotConfig       `yaml:"bots" json:"bots"`
	Web         WebConfig         `yaml:"web" json:"web"`
}

type StreamerConfig struct {
	Name        string         `yaml:"name" json:"name"`
	RoomID      int64          `yaml:"room_id" json:"room_id"`
	SourceLang  string         `yaml:"source_lang" json:"source_lang"`
	AltLangs    []string       `yaml:"alt_langs" json:"alt_langs"`
	Outputs     []OutputConfig `yaml:"outputs" json:"outputs"`
	CommandUIDs []int64        `yaml:"command_uids" json:"command_uids"` // UIDs allowed to send commands via danmaku
}

type STTConfig struct {
	Provider    string `yaml:"provider" json:"provider"`
	Credentials string `yaml:"credentials" json:"credentials"`
}

type TranslationConfig struct {
	Provider string `yaml:"provider" json:"provider"`
	APIKey   string `yaml:"api_key" json:"api_key"`
	Model    string `yaml:"model" json:"model"`
}

type OutputConfig struct {
	Name       string   `yaml:"name" json:"name"`
	Platform   string   `yaml:"platform" json:"platform"`
	TargetLang string   `yaml:"target_lang" json:"target_lang"`
	Account    string   `yaml:"account" json:"account"`       // single account (backward compat)
	Accounts   []string `yaml:"accounts" json:"accounts"`     // account pool for round-robin
	RoomID     int64    `yaml:"room_id" json:"room_id"`
	Prefix     string   `yaml:"prefix" json:"prefix"`
	Suffix     string   `yaml:"suffix" json:"suffix"`
	ShowSeq    bool     `yaml:"show_seq" json:"show_seq"`
}

// AccountPool returns the effective list of accounts for this output.
// If Accounts is set, use it; otherwise fall back to single Account.
func (o *OutputConfig) AccountPool() []string {
	if len(o.Accounts) > 0 {
		return o.Accounts
	}
	if o.Account != "" {
		return []string{o.Account}
	}
	return nil
}

type BotConfig struct {
	Name       string `yaml:"name" json:"name"`
	Platform   string `yaml:"platform" json:"platform"`
	SESSDATA   string `yaml:"sessdata" json:"sessdata"`
	BiliJCT    string `yaml:"bili_jct" json:"bili_jct"`
	UID        int64  `yaml:"uid" json:"uid"`
	DanmakuMax int    `yaml:"danmaku_max" json:"danmaku_max"`
}

type WebConfig struct {
	Port int        `yaml:"port" json:"port"`
	Auth AuthConfig `yaml:"auth" json:"auth"`
}

type AuthConfig struct {
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
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
		Web: WebConfig{
			Port: 8899,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Auto-migrate old single-streamer format
	if len(cfg.Streamers) == 0 {
		migrated := migrateOldFormat(data)
		if migrated != nil {
			cfg.Streamers = []StreamerConfig{*migrated}
			slog.Info("migrated old config format to multi-streamer", "streamer", migrated.Name)
			// Save in new format
			if err := Save(path, cfg); err != nil {
				slog.Warn("failed to save migrated config", "err", err)
			}
		}
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

	// Defaults for streamers and their outputs
	for i := range cfg.Streamers {
		s := &cfg.Streamers[i]
		if s.SourceLang == "" {
			s.SourceLang = "ja-JP"
		}
		if s.AltLangs == nil {
			s.AltLangs = []string{"en-US"}
		}
		for j := range s.Outputs {
			if s.Outputs[j].Platform == "" {
				s.Outputs[j].Platform = "bilibili"
			}
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

// migrateOldFormat attempts to parse the old single-streamer config format
// (with top-level "streamer" and "outputs" keys) and convert it.
func migrateOldFormat(data []byte) *StreamerConfig {
	var old struct {
		Streamer struct {
			Name       string   `yaml:"name"`
			RoomID     int64    `yaml:"room_id"`
			SourceLang string   `yaml:"source_lang"`
			AltLangs   []string `yaml:"alt_langs"`
		} `yaml:"streamer"`
		Outputs []OutputConfig `yaml:"outputs"`
	}
	if err := yaml.Unmarshal(data, &old); err != nil {
		return nil
	}
	if old.Streamer.RoomID == 0 {
		return nil
	}
	return &StreamerConfig{
		Name:       old.Streamer.Name,
		RoomID:     old.Streamer.RoomID,
		SourceLang: old.Streamer.SourceLang,
		AltLangs:   old.Streamer.AltLangs,
		Outputs:    old.Outputs,
	}
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

// RoomIDs returns all streamer room IDs.
func (c *Config) RoomIDs() []int64 {
	ids := make([]int64, 0, len(c.Streamers))
	for _, s := range c.Streamers {
		if s.RoomID != 0 {
			ids = append(ids, s.RoomID)
		}
	}
	return ids
}

// FindStreamer returns the streamer config for the given name.
func (c *Config) FindStreamer(name string) *StreamerConfig {
	for i := range c.Streamers {
		if c.Streamers[i].Name == name {
			return &c.Streamers[i]
		}
	}
	return nil
}

// FindStreamerByRoom returns the streamer config for the given room ID.
func (c *Config) FindStreamerByRoom(roomID int64) *StreamerConfig {
	for i := range c.Streamers {
		if c.Streamers[i].RoomID == roomID {
			return &c.Streamers[i]
		}
	}
	return nil
}
