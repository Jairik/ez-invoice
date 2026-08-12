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

// TestServeNormalizesBarePort verifies a bare port becomes a loopback address.
func TestServeNormalizesBarePort(t *testing.T) {
	if got := normalizeAddr(""); got != "127.0.0.1:9090" {
		t.Fatalf("normalizeAddr() = %q", got)
	}
	if got := normalizeAddr("8080"); got != "127.0.0.1:8080" {
		t.Fatalf("normalizeAddr(8080) = %q", got)
	}
	if got := normalizeAddr("0.0.0.0:9090"); got != "0.0.0.0:9090" {
		t.Fatalf("normalizeAddr(0.0.0.0:9090) = %q", got)
	}
	if got := normalizeAddr("::1:9090"); got != "::1:9090" {
		t.Fatalf("normalizeAddr(::1:9090) = %q", got)
	}
}
