// Package app bootstraps and coordinates the local ez-invoice application.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/store/sqlite"
)

// Paths contains all application-owned local files.
type Paths struct {
	DataDir      string
	ConfigFile   string
	DatabaseFile string
}

// App contains the loaded config and persistent store.
type App struct {
	Paths  Paths
	Store  *sqlite.Store
	config config.Config
	configMu sync.RWMutex
}

// Config returns a snapshot of the current application config.
func (app *App) Config() config.Config {
	app.configMu.RLock()
	defer app.configMu.RUnlock()
	return app.config
}

// SetConfig replaces the current application config.
func (app *App) SetConfig(cfg config.Config) {
	app.configMu.Lock()
	defer app.configMu.Unlock()
	app.config = cfg
}

// ResolvePaths applies a data-directory override or the platform default.
func ResolvePaths(dataDir string) (Paths, error) {
	if dataDir == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
		}
		dataDir = filepath.Join(root, "ez-invoice")
	}
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data directory: %w", err)
	}
	return Paths{
		DataDir: absolute, ConfigFile: filepath.Join(absolute, "config.toml"),
		DatabaseFile: filepath.Join(absolute, "invoices.db"),
	}, nil
}

// Open bootstraps config and storage.
func Open(dataDir string) (*App, error) {
	paths, err := ResolvePaths(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	cfg, _, err := config.Ensure(paths.ConfigFile, paths.DataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create invoice output directory: %w", err)
	}
	store, err := sqlite.Open(paths.DatabaseFile)
	if err != nil {
		return nil, err
	}
	application := &App{Paths: paths, Store: store}
	application.SetConfig(cfg)
	return application, nil
}

// Close releases app resources.
func (app *App) Close() error {
	if app.Store == nil {
		return nil
	}
	return app.Store.Close()
}
