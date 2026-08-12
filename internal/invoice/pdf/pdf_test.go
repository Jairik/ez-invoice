package pdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jairik/ez-invoice/internal/domain"
)

// pdfText inflates and concatenates every content stream so rendered text is
// searchable regardless of stream compression.
func pdfText(data []byte) ([]byte, error) {
	var text bytes.Buffer
	for {
		start := bytes.Index(data, []byte("stream"))
		if start == 0 {
			return nil, errors.New("unexpected stream at file start")
		}
		if start < 0 {
			return text.Bytes(), nil
		}
		body := data[start+len("stream"):]
		body = bytes.TrimPrefix(body, []byte("\r\n"))
		body = bytes.TrimPrefix(body, []byte("\n"))
		end := bytes.Index(body, []byte("endstream"))
		if end < 0 {
			return nil, errors.New("stream without endstream")
		}
		reader, err := zlib.NewReader(bytes.NewReader(body[:end]))
		if err != nil {
			return nil, fmt.Errorf("inflate stream: %w", err)
		}
		if _, err := io.Copy(&text, reader); err != nil {
			reader.Close()
			return nil, err
		}
		if err := reader.Close(); err != nil {
			return nil, err
		}
		data = body[end+len("endstream"):]
	}
}

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
		PeriodStart:    time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		PeriodEnd:      time.Date(2026, 8, 2, 23, 59, 0, 0, time.UTC),
		FromName:       "Ada Lovelace", FromAddress: "1 Computing Lane", FromEmail: "ada@example.com",
		ToCompany: "Analytical Engines", ToAddress: "2 Difference Road", PayableTerms: "Net 15", Currency: "USD",
		Notes: "Thank you", SubtotalCents: 20_875, AdjustmentCents: -500, TotalCents: 20_375,
		Contacts:  []domain.InvoiceContact{{Name: "Charles Babbage", Email: "charles@example.com"}},
		LineItems: []domain.InvoiceLineItem{{Description: "Development planning analysis design implementation testing and customer consultation", UnitPriceCents: 12_500, Units: 1.67, LineTotalCents: 20_875}},
	}
	// Use the supplied logo while exercising the PDF layout.
	invoice.LogoPath = filepath.Join("..", "..", "config", "tenaxiom-logo.png")
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
	text, err := pdfText(data)
	if err != nil {
		t.Fatalf("decompress PDF streams: %v", err)
	}
	for _, phrase := range []string{"Invoice", "Submitted on 08/05/2026", "Invoice From", "Ada Lovelace", "Invoice To", "Analytical Engines", "Point of Contact", "Invoice #", "Period", "7/14 - 8/2", "Payable", "Net 15", "Development", "consultation", "Subtotal", "Adjustment", "Total", "Thank you"} {
		if !bytes.Contains(text, []byte(phrase)) {
			t.Fatalf("rendered PDF does not contain %q", phrase)
		}
	}
}

// TestRenderTranslatesNonASCII verifies cp1252 text renders instead of raw UTF-8 bytes.
func TestRenderTranslatesNonASCII(t *testing.T) {
	sequence := int64(8)
	invoice := domain.Invoice{
		NumberSequence: &sequence,
		SubmittedDate:  time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		PeriodStart:    time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		PeriodEnd:      time.Date(2026, 8, 2, 23, 59, 0, 0, time.UTC),
		FromName:       "Café Léna", FromAddress: "Rue d'Amsterdam", FromEmail: "cafe@example.com",
		ToCompany: "Zürich GmbH", ToAddress: "Straße 5", PayableTerms: "Net 15", Currency: "USD",
		Notes: "Merci — rendez-vous ☕", SubtotalCents: 1_000, TotalCents: 1_000,
		LineItems: []domain.InvoiceLineItem{{Description: "Étude — consultation", UnitPriceCents: 1_000, Units: 1, LineTotalCents: 1_000}},
	}
	path := filepath.Join(t.TempDir(), Filename(invoice))
	if err := Render(invoice, path); err != nil {
		t.Fatalf("Render returned an error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned an error: %v", err)
	}
	// UTF-8 multi-byte sequences for these code points must not appear raw.
	for _, raw := range [][]byte{[]byte("Caf\xc3\xa9"), []byte("Z\xc3\xbcrich"), []byte("Stra\xc3\x9fe"), []byte("\xe2\x80\x94")} {
		if bytes.Contains(data, raw) {
			t.Fatalf("raw UTF-8 bytes %q still present in rendered PDF", raw)
		}
	}
}

// TestRenderChunksOversizedRows verifies rows taller than one page stay aligned.
func TestRenderChunksOversizedRows(t *testing.T) {
	sequence := int64(9)
	description := ""
	for len(description) < 5_000 {
		description += "Very long description line that keeps wrapping across the page. "
	}
	invoice := domain.Invoice{
		NumberSequence: &sequence,
		SubmittedDate:  time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
		PeriodStart:    time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		PeriodEnd:      time.Date(2026, 8, 2, 23, 59, 0, 0, time.UTC),
		FromName:       "Ada", FromAddress: "1 Lane", FromEmail: "a@example.com",
		ToCompany: "Engines", ToAddress: "2 Rd", PayableTerms: "Net 15", Currency: "USD",
		Notes: "Thanks", SubtotalCents: 1_000, TotalCents: 1_000,
		LineItems: []domain.InvoiceLineItem{{Description: description, UnitPriceCents: 1_000, Units: 1, LineTotalCents: 1_000}},
	}
	path := filepath.Join(t.TempDir(), Filename(invoice))
	if err := Render(invoice, path); err != nil {
		t.Fatalf("Render returned an error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned an error: %v", err)
	}
	if bytes.Count(data, []byte("/Type /Page")) < 2 {
		t.Fatalf("oversized row did not span multiple pages")
	}
}
