package pdf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jairik/ez-invoice/internal/domain"
)

// TestFilenameSanitizesManualNumbers verifies safe deterministic export names.
func TestFilenameSanitizesManualNumbers(t *testing.T) {
	invoice := domain.Invoice{NumberOverride: "ACME/2026 #7"}
	if got := Filename(invoice); got != "invoice-ACME-2026-7.pdf" {
		t.Fatalf("Filename = %q, want invoice-ACME-2026-7.pdf", got)
	}
}

// TestRenderWritesInvoiceSections verifies a direct, readable PDF export.
func TestRenderWritesInvoiceSections(t *testing.T) {
	sequence := int64(7)
	invoice := domain.Invoice{
		NumberSequence: &sequence,
		SubmittedDate:  time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		PeriodStart:    time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:      time.Date(2026, 8, 31, 23, 59, 0, 0, time.UTC),
		FromName:       "Ada Lovelace", FromAddress: "1 Computing Lane", FromEmail: "ada@example.com",
		ToCompany: "Analytical Engines", ToAddress: "2 Difference Road", PayableTerms: "Net 15", Currency: "USD",
		Notes: "Thank you", SubtotalCents: 20_875, AdjustmentCents: -500, TotalCents: 20_375,
		Contacts:  []domain.InvoiceContact{{Name: "Charles Babbage", Email: "charles@example.com"}},
		LineItems: []domain.InvoiceLineItem{{Description: "Development planning analysis design implementation testing and customer consultation", UnitPriceCents: 12_500, Units: 1.67, LineTotalCents: 20_875}},
	}
	path := filepath.Join(t.TempDir(), "nested", Filename(invoice))

	if err := Render(invoice, path); err != nil {
		t.Fatalf("Render returned an error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned an error: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) || len(data) < 1_000 {
		t.Fatalf("rendered file is not a non-trivial PDF: prefix=%q size=%d", data[:min(len(data), 5)], len(data))
	}
	for _, text := range []string{"INVOICE", "Ada Lovelace", "Analytical Engines", "Development", "consultation", "Subtotal", "Adjustment", "Total", "Thank you"} {
		if !bytes.Contains(data, []byte(text)) {
			t.Fatalf("rendered PDF does not contain %q", text)
		}
	}
}
