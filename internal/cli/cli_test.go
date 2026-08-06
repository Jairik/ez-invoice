package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/config"
)

// TestDefaultDateRange verifies time lists default to month-to-date.
func TestDefaultDateRange(t *testing.T) {
	from, to := defaultDateRange(time.Date(2026, 8, 5, 14, 0, 0, 0, time.Local))
	if from != "2026-08-01" || to != "2026-08-05" {
		t.Fatalf("defaultDateRange = %q to %q, want month-to-date", from, to)
	}
}

// TestConfigCommands verifies centralized profile editing and persistence.
func TestConfigCommands(t *testing.T) {
	application := openTestApp(t)
	for _, args := range [][]string{
		{"config", "set", "sender.name", "Ada Lovelace"},
		{"config", "set", "sender.address", "1 Computing Lane"},
		{"config", "set", "sender.email", "ada@example.com"},
		{"config", "set", "recipient.company", "Analytical Engines"},
		{"config", "set", "recipient.address", "2 Difference Road"},
		{"config", "contact", "add", "Charles Babbage", "charles@example.com"},
	} {
		if _, err := runTestCommand(application, args...); err != nil {
			t.Fatalf("Run(%v) returned an error: %v", args, err)
		}
	}

	loaded, err := config.Load(application.Paths.ConfigFile)
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if loaded.Sender.FullName != "Ada Lovelace" || loaded.Recipients[0].CompanyName != "Analytical Engines" || len(loaded.Contacts) != 1 {
		t.Fatalf("config commands were not persisted: %+v", loaded)
	}
	output, err := runTestCommand(application, "config", "show")
	if err != nil || !strings.Contains(output, "Ada Lovelace") || !strings.Contains(output, "Net 15") {
		t.Fatalf("config show output = %q, %v", output, err)
	}
}

// TestInvoiceCommands verifies preview, finalize, PDF export, and history helpers.
func TestInvoiceCommands(t *testing.T) {
	application := openTestApp(t)
	for _, args := range [][]string{
		{"config", "set", "sender.name", "Ada Lovelace"},
		{"config", "set", "sender.address", "1 Computing Lane"},
		{"config", "set", "sender.email", "ada@example.com"},
		{"config", "set", "recipient.company", "Analytical Engines"},
		{"config", "set", "recipient.address", "2 Difference Road"},
		{"time", "add", "--start", "2026-08-05T09:00:00Z", "--end", "2026-08-05T10:00:00Z", "--description", "Development", "--rate", "100.00"},
	} {
		if _, err := runTestCommand(application, args...); err != nil {
			t.Fatalf("Run(%v) returned an error: %v", args, err)
		}
	}

	output, err := runTestCommand(application, "invoice", "preview", "--from", "2026-08-05", "--to", "2026-08-05")
	if err != nil || !strings.Contains(output, "Development") || !strings.Contains(output, "100.00") {
		t.Fatalf("invoice preview output = %q, %v", output, err)
	}
	exportDir := filepath.Join(t.TempDir(), "exports")
	output, err = runTestCommand(application, "invoice", "generate", "--from", "2026-08-05", "--to", "2026-08-05", "--number", "ACME/7", "--output", exportDir)
	if err != nil || !strings.Contains(output, "ACME/7") {
		t.Fatalf("invoice generate output = %q, %v", output, err)
	}
	generatedPath := filepath.Join(exportDir, "invoice-ACME-7.pdf")
	if info, err := os.Stat(generatedPath); err != nil || info.Size() < 1_000 {
		t.Fatalf("generated PDF %q is missing or empty: info=%v err=%v", generatedPath, info, err)
	}
	output, err = runTestCommand(application, "invoice", "list")
	if err != nil || !strings.Contains(output, "ACME/7") || !strings.Contains(output, generatedPath) {
		t.Fatalf("invoice list output = %q, %v", output, err)
	}
	reexportDir := filepath.Join(t.TempDir(), "reexports")
	if _, err := runTestCommand(application, "invoice", "export", "1", "--output", reexportDir); err != nil {
		t.Fatalf("invoice export returned an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reexportDir, "invoice-ACME-7.pdf")); err != nil {
		t.Fatalf("re-exported PDF is missing: %v", err)
	}
}

// TestInvoiceGenerateReportsFinalizedExportFailure catches retryable-looking partial success.
func TestInvoiceGenerateReportsFinalizedExportFailure(t *testing.T) {
	application := openTestApp(t)
	application.Config.Sender = config.Sender{FullName: "Ada Lovelace", Address: "1 Computing Ln", Email: "ada@example.com"}
	application.Config.Recipients[0] = config.Recipient{CompanyName: "Analytical Engines", Address: "2 Difference Rd"}
	badLogo := filepath.Join(t.TempDir(), "not-an-image.txt")
	if err := os.WriteFile(badLogo, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	application.Config.LogoPath = badLogo
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatal(err)
	}
	if _, err := runTestCommand(application, "time", "add", "--start", "2026-08-06 09:00", "--end", "2026-08-06 10:00", "--description", "Development", "--rate", "100.00"); err != nil {
		t.Fatal(err)
	}
	_, err := runTestCommand(application, "invoice", "generate", "--from", "2026-08-06", "--to", "2026-08-06")
	var partial *FinalizedInvoiceError
	if !errors.As(err, &partial) || partial.InvoiceID != 1 {
		t.Fatalf("generate error=%T %v, want FinalizedInvoiceError for invoice 1", err, err)
	}
	invoices, listErr := application.Store.ListInvoices(context.Background())
	if listErr != nil || len(invoices) != 1 {
		t.Fatalf("finalized invoices=%+v err=%v", invoices, listErr)
	}
}

// TestPresetAndTimeCommands verifies non-TUI helpers for the primary tracking flow.
func TestPresetAndTimeCommands(t *testing.T) {
	application := openTestApp(t)
	commands := [][]string{
		{"rate", "add", "Standard", "125.00", "USD"},
		{"description", "add", "Development"},
		{"time", "add", "--start", "2026-08-05T09:00:00Z", "--end", "2026-08-05T10:40:00Z", "--description-preset", "1", "--rate-preset", "1"},
		{"time", "edit", "1", "--description", "Architecture", "--rate", "150.00"},
	}
	for _, args := range commands {
		if _, err := runTestCommand(application, args...); err != nil {
			t.Fatalf("Run(%v) returned an error: %v", args, err)
		}
	}

	output, err := runTestCommand(application, "time", "list", "--from", "2026-08-05", "--to", "2026-08-05")
	if err != nil || !strings.Contains(output, "Architecture") || !strings.Contains(output, "1.67") || !strings.Contains(output, "250.50") {
		t.Fatalf("time list output = %q, %v", output, err)
	}
	if _, err := runTestCommand(application, "rate", "delete", "1"); err != nil {
		t.Fatalf("rate delete returned an error: %v", err)
	}
	output, err = runTestCommand(application, "rate", "list", "--all")
	if err != nil || !strings.Contains(output, "Standard") || !strings.Contains(output, "false") {
		t.Fatalf("rate list --all output = %q, %v", output, err)
	}
	if _, err := runTestCommand(application, "time", "delete", "1"); err != nil {
		t.Fatalf("time delete returned an error: %v", err)
	}
	if _, err := runTestCommand(application, "time", "add", "--start", "2026-08-06T09:00:00Z", "--end", "2026-08-06T10:00:00Z", "--description", "Pro bono", "--rate", "0"); err != nil {
		t.Fatalf("zero-rate time add returned an error: %v", err)
	}
	if _, err := runTestCommand(application, "time", "edit", "2", "--description", "Community support"); err != nil {
		t.Fatalf("partial zero-rate time edit returned an error: %v", err)
	}
}

// openTestApp creates isolated config and storage for CLI tests.
func openTestApp(t *testing.T) *app.App {
	t.Helper()
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("app.Open returned an error: %v", err)
	}
	t.Cleanup(func() { application.Close() })
	return application
}

// runTestCommand captures one command's standard output.
func runTestCommand(application *app.App, args ...string) (string, error) {
	var output bytes.Buffer
	err := Run(context.Background(), application, args, &output, &output)
	return output.String(), err
}
