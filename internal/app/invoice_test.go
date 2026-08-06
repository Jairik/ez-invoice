package app

import (
	"context"
	"testing"
	"time"

	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// TestInvoiceAssembly verifies row exclusion, totals, and immutable config snapshots.
func TestInvoiceAssembly(t *testing.T) {
	ctx := context.Background()
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer application.Close()

	application.Config.Sender = config.Sender{FullName: "Ada Lovelace", Address: "1 Computing Ln", Email: "ada@example.com"}
	application.Config.Recipients[0] = config.Recipient{CompanyName: "Analytical Engines", Address: "2 Difference Rd"}
	application.Config.Contacts = []config.Contact{{Name: "Charles Babbage", Email: "charles@example.com"}}
	application.Config.DefaultAdjustment = "-5.00"
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatalf("Save returned an error: %v", err)
	}

	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	first, err := application.Store.CreateTimeEntry(ctx, domain.TimeEntry{
		StartAt: start, EndAt: start.Add(time.Hour), Description: "Development", RateAmountCents: 10_000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Store.CreateTimeEntry(ctx, domain.TimeEntry{
		StartAt: start.Add(2 * time.Hour), EndAt: start.Add(3 * time.Hour), Description: "Review", RateAmountCents: 8_000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	options := InvoiceOptions{From: start.Add(-time.Hour), To: start.Add(24 * time.Hour), ExcludeIDs: []int64{second.ID}}
	application.Config.Recipients = append(application.Config.Recipients, config.Recipient{})
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatal(err)
	}
	invalidRecipient := options
	invalidRecipient.RecipientIndex = 1
	if _, err := application.PreviewInvoice(ctx, invalidRecipient); err == nil {
		t.Fatal("PreviewInvoice accepted an incomplete selected recipient")
	}
	invalidContacts := options
	invalidContacts.Contacts = []config.Contact{{Name: "Invalid", Email: "not-an-email"}}
	if _, err := application.PreviewInvoice(ctx, invalidContacts); err == nil {
		t.Fatal("PreviewInvoice accepted an invalid contact override")
	}

	preview, err := application.PreviewInvoice(ctx, options)
	if err != nil {
		t.Fatalf("PreviewInvoice returned an error: %v", err)
	}
	if len(preview.Entries) != 1 || preview.Entries[0].ID != first.ID || preview.SubtotalCents != 10_000 || preview.TotalCents != 9_500 {
		t.Fatalf("preview = %+v; want one selected row totaling 95.00 after adjustment", preview)
	}

	invoice, err := application.FinalizeInvoice(ctx, options)
	if err != nil {
		t.Fatalf("FinalizeInvoice returned an error: %v", err)
	}
	if invoice.FromName != "Ada Lovelace" || invoice.ToCompany != "Analytical Engines" || invoice.TotalCents != 9_500 || len(invoice.Contacts) != 1 {
		t.Fatalf("final invoice did not snapshot config and totals: %+v", invoice)
	}

	application.Config.Sender.FullName = "Changed Later"
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatal(err)
	}
	loaded, err := application.Store.GetInvoice(ctx, invoice.ID)
	if err != nil || loaded.FromName != "Ada Lovelace" {
		t.Fatalf("past invoice changed with config: %+v, %v", loaded, err)
	}
}

// TestInvoiceUsesSelectedRecipient catches an incomplete first profile blocking a valid later choice.
func TestInvoiceUsesSelectedRecipient(t *testing.T) {
	ctx := context.Background()
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	application.Config.Sender = config.Sender{FullName: "Ada Lovelace", Address: "1 Computing Ln", Email: "ada@example.com"}
	application.Config.Recipients = append(application.Config.Recipients, config.Recipient{CompanyName: "Analytical Engines", Address: "2 Difference Rd"})
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 6, 9, 0, 0, 0, time.Local)
	if _, err := application.Store.CreateTimeEntry(ctx, domain.TimeEntry{
		StartAt: start, EndAt: start.Add(time.Hour), Description: "Development", RateAmountCents: 10_000, Currency: "USD",
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := application.PreviewInvoice(ctx, InvoiceOptions{From: start.Add(-time.Hour), To: start.Add(2 * time.Hour), RecipientIndex: 1})
	if err != nil || len(preview.Entries) != 1 {
		t.Fatalf("selected second recipient preview=%+v err=%v", preview, err)
	}
}
