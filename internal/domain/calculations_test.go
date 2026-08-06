package domain

import (
	"testing"
	"time"
)

// TestHours verifies the shared duration rule and its invalid boundary.
func TestHours(t *testing.T) {
	start := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	got, err := Hours(start, start.Add(100*time.Minute))
	if err != nil {
		t.Fatalf("Hours returned an error: %v", err)
	}
	if got != 1.67 {
		t.Fatalf("Hours = %v, want 1.67", got)
	}

	if _, err := Hours(start, start); err == nil {
		t.Fatal("Hours accepted an end time equal to the start time")
	}
}

// TestLineTotal verifies cent rounding and negative-input rejection.
func TestLineTotal(t *testing.T) {
	got, err := LineTotal(12_550, 1.67)
	if err != nil {
		t.Fatalf("LineTotal returned an error: %v", err)
	}
	if got != 20_959 {
		t.Fatalf("LineTotal = %d, want 20959", got)
	}

	if _, err := LineTotal(-1, 1); err == nil {
		t.Fatal("LineTotal accepted a negative rate")
	}
}

// TestMoneyRoundTrip verifies user-facing decimal parsing and formatting.
func TestMoneyRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		input string
		cents int64
		text  string
	}{
		{input: "0", cents: 0, text: "0.00"},
		{input: "125.5", cents: 12_550, text: "125.50"},
		{input: "-10.25", cents: -1_025, text: "-10.25"},
	} {
		got, err := ParseMoney(tc.input)
		if err != nil {
			t.Fatalf("ParseMoney(%q) returned an error: %v", tc.input, err)
		}
		if got != tc.cents || FormatMoney(got) != tc.text {
			t.Fatalf("money round trip for %q = (%d, %q), want (%d, %q)", tc.input, got, FormatMoney(got), tc.cents, tc.text)
		}
	}

	if _, err := ParseMoney("12.345"); err == nil {
		t.Fatal("ParseMoney accepted more than two decimal places")
	}
}
