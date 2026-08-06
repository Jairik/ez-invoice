package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureRoundTrip verifies first-run defaults and persisted edits.
func TestEnsureRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "config.toml")

	cfg, created, err := Ensure(path, dataDir)
	if err != nil {
		t.Fatalf("Ensure returned an error: %v", err)
	}
	if !created || cfg.Currency != "USD" || cfg.PayableTerms != "Net 15" || cfg.Notes != "None" {
		t.Fatalf("unexpected first-run config: created=%v config=%+v", created, cfg)
	}
	if cfg.OutputDir != filepath.Join(dataDir, "invoices") {
		t.Fatalf("OutputDir = %q, want a directory below the data directory", cfg.OutputDir)
	}

	cfg.Sender.FullName = "Ada Lovelace"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save returned an error: %v", err)
	}
	if info, err := os.Stat(cfg.OutputDir); err != nil || !info.IsDir() {
		t.Fatalf("Save did not create the configured output directory: info=%v err=%v", info, err)
	}
	loaded, created, err := Ensure(path, dataDir)
	if err != nil {
		t.Fatalf("second Ensure returned an error: %v", err)
	}
	if created || loaded.Sender.FullName != "Ada Lovelace" {
		t.Fatalf("persisted config was not loaded: created=%v config=%+v", created, loaded)
	}
}

// TestLoadRejectsUnknownFields verifies strict schema validation.
func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("currency = \"USD\"\nunknown = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Load error = %v, want an unknown-field error", err)
	}
}

// TestValidateForInvoice verifies required sender, contact, and output values.
func TestValidateForInvoice(t *testing.T) {
	cfg := Default(t.TempDir())
	if err := cfg.ValidateForInvoice(); err == nil {
		t.Fatal("ValidateForInvoice accepted an empty sender profile")
	}

	cfg.Sender = Sender{FullName: "Ada Lovelace", Address: "1 Computing Ln", Email: "ada@example.com"}
	cfg.Recipients[0] = Recipient{CompanyName: "Analytical Engines", Address: "2 Difference Rd"}
	cfg.Contacts = []Contact{{Name: "Charles Babbage", Email: "charles@example.com"}}
	if err := cfg.ValidateForInvoice(); err != nil {
		t.Fatalf("ValidateForInvoice rejected a valid config: %v", err)
	}
	if info, err := os.Stat(cfg.OutputDir); err != nil || !info.IsDir() {
		t.Fatalf("output directory was not created: info=%v err=%v", info, err)
	}
}
