package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunBootstrapsAndRoutesCLI verifies global data-dir handling and command routing.
func TestRunBootstrapsAndRoutesCLI(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	var output bytes.Buffer

	if err := run([]string{"--data-dir", dataDir, "help"}, &output, &output); err != nil {
		t.Fatalf("run returned an error: %v", err)
	}
	if !strings.Contains(output.String(), "ez-invoice commands") {
		t.Fatalf("help output = %q", output.String())
	}
	for _, name := range []string{"config.toml", "invoices.db"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
			t.Fatalf("bootstrap file %q is missing: %v", name, err)
		}
	}
}
