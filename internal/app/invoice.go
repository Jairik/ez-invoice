package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// InvoiceOptions controls row selection and metadata overrides.
type InvoiceOptions struct {
	From            time.Time
	To              time.Time
	IncludeIDs      []int64
	ExcludeIDs      []int64
	SubmittedDate   time.Time
	RecipientIndex  int
	NumberOverride  string
	PayableTerms    string
	Notes           string
	AdjustmentCents *int64
	Contacts        []config.Contact
}

// InvoicePreview is the selected table and its calculated totals.
type InvoicePreview struct {
	Entries         []domain.TimeEntry
	SubtotalCents   int64
	AdjustmentCents int64
	TotalCents      int64
}

// PreviewInvoice assembles selected rows without persisting an invoice.
func (app *App) PreviewInvoice(ctx context.Context, options InvoiceOptions) (InvoicePreview, error) {
	_, preview, err := app.assembleInvoice(ctx, options)
	return preview, err
}

// FinalizeInvoice snapshots selected rows and allocates its number.
func (app *App) FinalizeInvoice(ctx context.Context, options InvoiceOptions) (domain.Invoice, error) {
	draft, preview, err := app.assembleInvoice(ctx, options)
	if err != nil {
		return domain.Invoice{}, err
	}
	if len(preview.Entries) == 0 {
		return domain.Invoice{}, errors.New("no selected uninvoiced time entries")
	}
	return app.Store.FinalizeInvoice(ctx, draft)
}

// assembleInvoice reloads config and builds one consistent preview and draft.
func (app *App) assembleInvoice(ctx context.Context, options InvoiceOptions) (domain.InvoiceDraft, InvoicePreview, error) {
	cfg, err := config.Load(app.Paths.ConfigFile)
	if err != nil {
		return domain.InvoiceDraft{}, InvoicePreview{}, err
	}
	if err := cfg.ValidateForInvoice(); err != nil {
		return domain.InvoiceDraft{}, InvoicePreview{}, err
	}
	app.SetConfig(cfg)
	if !options.To.After(options.From) {
		return domain.InvoiceDraft{}, InvoicePreview{}, errors.New("invoice range end must be after its start")
	}
	if options.RecipientIndex < 0 || options.RecipientIndex >= len(cfg.Recipients) {
		return domain.InvoiceDraft{}, InvoicePreview{}, fmt.Errorf("recipient number must be between 1 and %d", len(cfg.Recipients))
	}
	recipient := cfg.Recipients[options.RecipientIndex]
	if strings.TrimSpace(recipient.CompanyName) == "" || strings.TrimSpace(recipient.Address) == "" {
		return domain.InvoiceDraft{}, InvoicePreview{}, errors.New("selected recipient company name and address are required")
	}

	entries, err := app.Store.ListTimeEntries(ctx, options.From, options.To, true)
	if err != nil {
		return domain.InvoiceDraft{}, InvoicePreview{}, err
	}
	excluded := make(map[int64]bool, len(options.ExcludeIDs))
	for _, id := range options.ExcludeIDs {
		excluded[id] = true
	}
	var included map[int64]bool
	if options.IncludeIDs != nil {
		included = make(map[int64]bool, len(options.IncludeIDs))
		for _, id := range options.IncludeIDs {
			included[id] = true
		}
	}
	found := make(map[int64]bool, len(included))
	preview := InvoicePreview{}
	var entryIDs []int64
	for _, entry := range entries {
		if included != nil && !included[entry.ID] {
			continue
		}
		found[entry.ID] = true
		if excluded[entry.ID] {
			continue
		}
		if entry.Currency != cfg.Currency {
			return domain.InvoiceDraft{}, InvoicePreview{}, fmt.Errorf("time entry %d currency %s does not match configured invoice currency %s", entry.ID, entry.Currency, cfg.Currency)
		}
		preview.Entries = append(preview.Entries, entry)
		entryIDs = append(entryIDs, entry.ID)
		if lineTotal := entry.LineTotalCents(); lineTotal > 0 && preview.SubtotalCents > math.MaxInt64-lineTotal {
			return domain.InvoiceDraft{}, InvoicePreview{}, errors.New("invoice subtotal is too large")
		} else {
			preview.SubtotalCents += lineTotal
		}
	}
	for id := range included {
		if !found[id] {
			return domain.InvoiceDraft{}, InvoicePreview{}, fmt.Errorf("previewed time entry %d is no longer available", id)
		}
	}
	preview.AdjustmentCents, err = domain.ParseMoney(cfg.DefaultAdjustment)
	if err != nil {
		return domain.InvoiceDraft{}, InvoicePreview{}, err
	}
	if options.AdjustmentCents != nil {
		preview.AdjustmentCents = *options.AdjustmentCents
	}
	if preview.AdjustmentCents > 0 && preview.SubtotalCents > math.MaxInt64-preview.AdjustmentCents {
		return domain.InvoiceDraft{}, InvoicePreview{}, errors.New("invoice total is too large")
	}
	preview.TotalCents = preview.SubtotalCents + preview.AdjustmentCents

	submitted := options.SubmittedDate
	if submitted.IsZero() {
		submitted = time.Now()
	}
	terms := options.PayableTerms
	if terms == "" {
		terms = cfg.PayableTerms
	}
	notes := options.Notes
	if notes == "" {
		notes = cfg.Notes
	}
	contacts := cfg.Contacts
	if options.Contacts != nil {
		contacts = options.Contacts
	}
	contactConfig := cfg
	contactConfig.Contacts = contacts
	if err := contactConfig.ValidateForInvoice(); err != nil {
		return domain.InvoiceDraft{}, InvoicePreview{}, err
	}
	invoiceContacts := make([]domain.InvoiceContact, 0, len(contacts))
	for _, contact := range contacts {
		invoiceContacts = append(invoiceContacts, domain.InvoiceContact{Name: contact.Name, Email: contact.Email})
	}
	draft := domain.InvoiceDraft{
		EntryIDs: entryIDs, NumberOverride: options.NumberOverride, SubmittedDate: submitted,
		PeriodStart: options.From, PeriodEnd: options.To.Add(-time.Nanosecond),
		FromName: cfg.Sender.FullName, FromAddress: cfg.Sender.Address, FromEmail: cfg.Sender.Email,
		ToCompany: recipient.CompanyName, ToAddress: recipient.Address,
		PayableTerms: terms, Currency: cfg.Currency, Notes: notes,
		AdjustmentCents: preview.AdjustmentCents, LogoPath: cfg.LogoPath, Contacts: invoiceContacts,
	}
	return draft, preview, nil
}
