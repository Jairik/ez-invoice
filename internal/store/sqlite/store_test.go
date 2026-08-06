package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jairik/ez-invoice/internal/domain"
)

// TestPresetAndTimeEntryFlow verifies the editable preset and time-entry lifecycle.
func TestPresetAndTimeEntryFlow(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	rate, err := store.CreateRatePreset(ctx, domain.RatePreset{Label: "Standard", AmountCents: 12_500, Currency: "USD", Active: true})
	if err != nil {
		t.Fatalf("CreateRatePreset returned an error: %v", err)
	}
	description, err := store.CreateDescriptionPreset(ctx, domain.DescriptionPreset{Label: "Development", Active: true})
	if err != nil {
		t.Fatalf("CreateDescriptionPreset returned an error: %v", err)
	}
	rate.Label = "Consulting"
	if rate, err = store.UpdateRatePreset(ctx, rate); err != nil || rate.Label != "Consulting" {
		t.Fatalf("UpdateRatePreset = %+v, %v", rate, err)
	}
	description.Label = "Software development"
	if description, err = store.UpdateDescriptionPreset(ctx, description); err != nil || description.Label != "Software development" {
		t.Fatalf("UpdateDescriptionPreset = %+v, %v", description, err)
	}

	start := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	entry, err := store.CreateTimeEntry(ctx, domain.TimeEntry{
		StartAt: start, EndAt: start.Add(100 * time.Minute), Description: description.Label,
		RateAmountCents: rate.AmountCents, Currency: "USD", DescriptionPresetID: &description.ID, RatePresetID: &rate.ID,
	})
	if err != nil {
		t.Fatalf("CreateTimeEntry returned an error: %v", err)
	}
	if entry.Hours != 1.67 || entry.LineTotalCents() != 20_875 {
		t.Fatalf("derived entry values = hours %v total %d, want 1.67 and 20875", entry.Hours, entry.LineTotalCents())
	}

	entries, err := store.ListTimeEntries(ctx, start.Add(-time.Hour), start.Add(24*time.Hour), true)
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("ListTimeEntries = %+v, %v; want the created entry", entries, err)
	}
	outside, err := store.ListTimeEntries(ctx, start.Add(24*time.Hour), start.Add(48*time.Hour), false)
	if err != nil || len(outside) != 0 {
		t.Fatalf("out-of-range ListTimeEntries = %+v, %v; want no entries", outside, err)
	}

	entry.Description = "Architecture"
	if _, err := store.UpdateTimeEntry(ctx, entry); err != nil {
		t.Fatalf("UpdateTimeEntry returned an error: %v", err)
	}
	loaded, err := store.GetTimeEntry(ctx, entry.ID)
	if err != nil || loaded.Description != "Architecture" {
		t.Fatalf("GetTimeEntry = %+v, %v; want the edited entry", loaded, err)
	}
	if err := store.SetRatePresetActive(ctx, rate.ID, false); err != nil {
		t.Fatalf("SetRatePresetActive returned an error: %v", err)
	}
	rates, err := store.ListRatePresets(ctx, false)
	if err != nil || len(rates) != 0 {
		t.Fatalf("active ListRatePresets = %+v, %v; want no active rates", rates, err)
	}
	if err := store.DeleteTimeEntry(ctx, entry.ID); err != nil {
		t.Fatalf("DeleteTimeEntry returned an error: %v", err)
	}
}

// TestFinalizeInvoice verifies totals, snapshots, linkage, and number allocation.
func TestFinalizeInvoice(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

	firstEntry := createTestEntry(t, store, start, "Development", 10_000)
	firstDraft := testDraft(start, []int64{firstEntry.ID}, "  ")
	firstDraft.SubmittedDate = time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("UTC+10", 10*60*60))
	first, err := store.FinalizeInvoice(ctx, firstDraft)
	if err != nil {
		t.Fatalf("first FinalizeInvoice returned an error: %v", err)
	}
	if first.NumberSequence == nil || *first.NumberSequence != 1 || first.DisplayNumber() != "1" {
		t.Fatalf("first invoice number = seq %v display %q, want 1", first.NumberSequence, first.DisplayNumber())
	}
	if first.SubtotalCents != 10_000 || first.AdjustmentCents != -500 || first.TotalCents != 9_500 {
		t.Fatalf("first totals = %d, %d, %d; want 10000, -500, 9500", first.SubtotalCents, first.AdjustmentCents, first.TotalCents)
	}
	if len(first.LineItems) != 1 || first.LineItems[0].SourceTimeEntryID == nil || len(first.Contacts) != 1 {
		t.Fatalf("invoice relations were not snapshotted: %+v", first)
	}

	available, err := store.ListTimeEntries(ctx, start.Add(-time.Hour), start.Add(48*time.Hour), true)
	if err != nil || len(available) != 0 {
		t.Fatalf("finalized entries remain available: %+v, %v", available, err)
	}
	if _, err := store.FinalizeInvoice(ctx, testDraft(start, []int64{firstEntry.ID}, "again")); err == nil {
		t.Fatal("FinalizeInvoice accepted an already-invoiced time entry")
	}

	overrideEntry := createTestEntry(t, store, start.Add(2*time.Hour), "Review", 8_000)
	override, err := store.FinalizeInvoice(ctx, testDraft(start, []int64{overrideEntry.ID}, "CUSTOM-7"))
	if err != nil || override.NumberSequence != nil || override.DisplayNumber() != "CUSTOM-7" {
		t.Fatalf("override invoice = %+v, %v; want CUSTOM-7 without a sequence", override, err)
	}

	thirdEntry := createTestEntry(t, store, start.Add(4*time.Hour), "Testing", 7_500)
	third, err := store.FinalizeInvoice(ctx, testDraft(start, []int64{thirdEntry.ID}, ""))
	if err != nil || third.NumberSequence == nil || *third.NumberSequence != 2 {
		t.Fatalf("next sequential invoice = %+v, %v; want sequence 2", third, err)
	}

	loaded, err := store.GetInvoice(ctx, first.ID)
	if err != nil || loaded.FromName != "Ada Lovelace" || loaded.ToCompany != "Analytical Engines" || len(loaded.LineItems) != 1 || loaded.SubmittedDate.Format("2006-01-02") != "2026-07-01" {
		t.Fatalf("GetInvoice did not preserve the snapshot: %+v, %v", loaded, err)
	}
	if err := store.SetInvoicePDFPath(ctx, first.ID, "/tmp/invoice-1.pdf"); err != nil {
		t.Fatalf("SetInvoicePDFPath returned an error: %v", err)
	}
	loaded, err = store.GetInvoice(ctx, first.ID)
	if err != nil || loaded.PDFPath != "/tmp/invoice-1.pdf" {
		t.Fatalf("stored PDF path = %q, %v", loaded.PDFPath, err)
	}
	invoices, err := store.ListInvoices(ctx)
	if err != nil || len(invoices) != 3 || invoices[0].ID != third.ID {
		t.Fatalf("ListInvoices = %+v, %v; want three newest-first invoices", invoices, err)
	}
}

// openTestStore creates a migrated database for one integration test.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "invoice.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// createTestEntry stores a one-hour uninvoiced entry.
func createTestEntry(t *testing.T, store *Store, start time.Time, description string, rate int64) domain.TimeEntry {
	t.Helper()
	entry, err := store.CreateTimeEntry(context.Background(), domain.TimeEntry{
		StartAt: start, EndAt: start.Add(time.Hour), Description: description, RateAmountCents: rate, Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateTimeEntry returned an error: %v", err)
	}
	return entry
}

// testDraft returns a complete invoice snapshot around selected entries.
func testDraft(start time.Time, entryIDs []int64, override string) domain.InvoiceDraft {
	return domain.InvoiceDraft{
		EntryIDs: entryIDs, NumberOverride: override, SubmittedDate: start,
		PeriodStart: start, PeriodEnd: start.Add(24 * time.Hour),
		FromName: "Ada Lovelace", FromAddress: "1 Computing Ln", FromEmail: "ada@example.com",
		ToCompany: "Analytical Engines", ToAddress: "2 Difference Rd",
		PayableTerms: "Net 15", Currency: "USD", Notes: "None", AdjustmentCents: -500,
		Contacts: []domain.InvoiceContact{{Name: "Charles Babbage", Email: "charles@example.com"}},
	}
}
