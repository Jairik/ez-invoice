package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenBootstrapsLocalFiles verifies first-run config and database creation.
func TestOpenBootstrapsLocalFiles(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "ez-invoice")
	application, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer application.Close()

	if application.Paths.DataDir != dataDir || application.Config.Currency != "USD" {
		t.Fatalf("unexpected bootstrapped app: paths=%+v config=%+v", application.Paths, application.Config)
	}
	if application.Config.Sender.FullName != "Jairik McCauley" || application.Config.Recipients[0].CompanyName != "Tenaxiom Technology, Inc" {
		t.Fatalf("unexpected Tenaxiom defaults: %+v", application.Config)
	}
	if info, err := os.Stat(application.Config.LogoPath); err != nil || info.IsDir() {
		t.Fatalf("default logo was not created: info=%v err=%v", info, err)
	}
	for _, path := range []string{application.Paths.ConfigFile, application.Paths.DatabaseFile} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			t.Fatalf("expected file %q was not created: info=%v err=%v", path, info, err)
		}
	}
	if info, err := os.Stat(application.Config.OutputDir); err != nil || !info.IsDir() {
		t.Fatalf("default output directory was not created: info=%v err=%v", info, err)
	}
}

// TestResolvePathsUsesOverride verifies deterministic app-data placement.
func TestResolvePathsUsesOverride(t *testing.T) {
	paths, err := ResolvePaths("/tmp/custom-ez-invoice")
	if err != nil {
		t.Fatalf("ResolvePaths returned an error: %v", err)
	}
	if paths.ConfigFile != "/tmp/custom-ez-invoice/config.toml" || paths.DatabaseFile != "/tmp/custom-ez-invoice/invoices.db" {
		t.Fatalf("ResolvePaths = %+v", paths)
	}
}
