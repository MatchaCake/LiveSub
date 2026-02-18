package config

import (
	"log/slog"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// HotConfig wraps Config with hot-reload support
type HotConfig struct {
	mu   sync.RWMutex
	cfg  *Config
	path string
	subs []func(*Config)
}

func NewHotConfig(path string) (*HotConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &HotConfig{cfg: cfg, path: path}, nil
}

func (hc *HotConfig) Get() *Config {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.cfg
}

// OnReload registers a callback for config changes
func (hc *HotConfig) OnReload(fn func(*Config)) {
	hc.subs = append(hc.subs, fn)
}

func (hc *HotConfig) reload() {
	cfg, err := Load(hc.path)
	if err != nil {
		slog.Error("config reload failed", "err", err)
		return
	}
	hc.mu.Lock()
	hc.cfg = cfg
	hc.mu.Unlock()

	slog.Info("🔄 config reloaded", "path", hc.path)
	for _, fn := range hc.subs {
		fn(cfg)
	}
}

// Watch starts watching the config file for changes
func (hc *HotConfig) Watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("config watcher failed", "err", err)
		return
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					hc.reload()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("config watcher error", "err", err)
			}
		}
	}()

	if err := watcher.Add(hc.path); err != nil {
		slog.Error("watch config file failed", "path", hc.path, "err", err)
	}
}
