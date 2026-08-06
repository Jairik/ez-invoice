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
	if cfg.Sender != (Sender{FullName: "Jairik McCauley", Address: "11223 Gehr Rd, Big Pool MD 21711", Email: "mjairik@gmail.com"}) {
		t.Fatalf("unexpected default sender: %+v", cfg.Sender)
	}
	if cfg.Recipients[0] != (Recipient{CompanyName: "Tenaxiom Technology, Inc", Address: "7459 Digby Grn\nAlexandria, VA 22315"}) {
		t.Fatalf("unexpected default recipient: %+v", cfg.Recipients[0])
	}
	if len(cfg.Contacts) != 2 || cfg.Contacts[0] != (Contact{Name: "Amy Marden", Email: "amy.marden@tenaxiom.tech"}) || cfg.Contacts[1] != (Contact{Name: "Tito Torres", Email: "tito.torres@tenaxiom.tech"}) {
		t.Fatalf("unexpected default contacts: %+v", cfg.Contacts)
	}
	if cfg.LogoPath != filepath.Join(dataDir, "tenaxiom-logo.png") {
		t.Fatalf("LogoPath = %q, want the bundled logo in the data directory", cfg.LogoPath)
	}
	if info, err := os.Stat(cfg.LogoPath); err != nil || info.IsDir() {
		t.Fatalf("default logo was not created: info=%v err=%v", info, err)
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
	cfg.LogoPath = ""
	if err := cfg.ValidateForInvoice(); err != nil {
		t.Fatalf("ValidateForInvoice rejected a valid config: %v", err)
	}
	if info, err := os.Stat(cfg.OutputDir); err != nil || !info.IsDir() {
		t.Fatalf("output directory was not created: info=%v err=%v", info, err)
	}
}
