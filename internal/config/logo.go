package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// bundledLogo contains the supplied Tenaxiom logo for first-run setup.
//
//go:embed tenaxiom-logo.png
var bundledLogo []byte

// DefaultLogoPath returns the stable path used by new invoice snapshots.
func DefaultLogoPath(dataDir string) string {
	return filepath.Join(dataDir, "tenaxiom-logo.png")
}

// ensureDefaultLogo materializes the embedded logo without overwriting it.
func ensureDefaultLogo(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("default logo path is a directory: %s", path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect default logo: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create logo directory: %w", err)
	}
	if err := os.WriteFile(path, bundledLogo, 0o600); err != nil {
		return fmt.Errorf("write default logo: %w", err)
	}
	return nil
}
