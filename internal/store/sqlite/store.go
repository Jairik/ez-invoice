// Package sqlite persists ez-invoice data in a local SQLite database.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Jairik/ez-invoice/internal/domain"
)

// Store owns the SQLite connection.
type Store struct {
	db *sql.DB
}

// Open opens and migrates the local database.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// WAL + synchronous=NORMAL skips a journal fsync per write while staying
	// crash-safe for a single local connection.
	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?_foreign_keys=on&_busy_timeout=5000&_txlock=immediate&_journal_mode=WAL&_synchronous=NORMAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// ponytail: one connection keeps local writes and number allocation predictable.
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close closes the database connection.
func (store *Store) Close() error { return store.db.Close() }

// CreateRatePreset creates an active or inactive rate preset.
func (store *Store) CreateRatePreset(ctx context.Context, preset domain.RatePreset) (domain.RatePreset, error) {
	if err := validateRatePreset(preset); err != nil {
		return domain.RatePreset{}, err
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO rate_presets(label, amount, currency, active) VALUES (?, ?, ?, ?)`,
		strings.TrimSpace(preset.Label), preset.AmountCents, strings.TrimSpace(preset.Currency), preset.Active)
	if err != nil {
		return domain.RatePreset{}, fmt.Errorf("create rate preset: %w", err)
	}
	preset.ID, err = result.LastInsertId()
	return preset, err
}

// UpdateRatePreset updates a reusable rate.
func (store *Store) UpdateRatePreset(ctx context.Context, preset domain.RatePreset) (domain.RatePreset, error) {
	if err := validateRatePreset(preset); err != nil {
		return domain.RatePreset{}, err
	}
	result, err := store.db.ExecContext(ctx, `UPDATE rate_presets SET label = ?, amount = ?, currency = ?, active = ? WHERE id = ?`,
		strings.TrimSpace(preset.Label), preset.AmountCents, strings.TrimSpace(preset.Currency), preset.Active, preset.ID)
	if err != nil {
		return domain.RatePreset{}, fmt.Errorf("update rate preset: %w", err)
	}
	if err := requireChanged(result, "rate preset"); err != nil {
		return domain.RatePreset{}, err
	}
	return preset, nil
}

// ListRatePresets lists rate presets.
func (store *Store) ListRatePresets(ctx context.Context, includeInactive bool) ([]domain.RatePreset, error) {
	query := `SELECT id, label, amount, currency, active FROM rate_presets`
	if !includeInactive {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY label`
	rows, err := store.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list rate presets: %w", err)
	}
	defer rows.Close()
	var presets []domain.RatePreset
	for rows.Next() {
		var preset domain.RatePreset
		if err := rows.Scan(&preset.ID, &preset.Label, &preset.AmountCents, &preset.Currency, &preset.Active); err != nil {
			return nil, fmt.Errorf("scan rate preset: %w", err)
		}
		presets = append(presets, preset)
	}
	return presets, rows.Err()
}

// SetRatePresetActive changes rate availability.
func (store *Store) SetRatePresetActive(ctx context.Context, id int64, active bool) error {
	result, err := store.db.ExecContext(ctx, `UPDATE rate_presets SET active = ? WHERE id = ?`, active, id)
	if err != nil {
		return fmt.Errorf("update rate preset status: %w", err)
	}
	return requireChanged(result, "rate preset")
}

// CreateDescriptionPreset creates a description preset.
func (store *Store) CreateDescriptionPreset(ctx context.Context, preset domain.DescriptionPreset) (domain.DescriptionPreset, error) {
	if strings.TrimSpace(preset.Label) == "" {
		return domain.DescriptionPreset{}, errors.New("description label is required")
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO description_presets(label, active) VALUES (?, ?)`, strings.TrimSpace(preset.Label), preset.Active)
	if err != nil {
		return domain.DescriptionPreset{}, fmt.Errorf("create description preset: %w", err)
	}
	preset.ID, err = result.LastInsertId()
	return preset, err
}

// UpdateDescriptionPreset updates a reusable description.
func (store *Store) UpdateDescriptionPreset(ctx context.Context, preset domain.DescriptionPreset) (domain.DescriptionPreset, error) {
	if strings.TrimSpace(preset.Label) == "" {
		return domain.DescriptionPreset{}, errors.New("description label is required")
	}
	result, err := store.db.ExecContext(ctx, `UPDATE description_presets SET label = ?, active = ? WHERE id = ?`, strings.TrimSpace(preset.Label), preset.Active, preset.ID)
	if err != nil {
		return domain.DescriptionPreset{}, fmt.Errorf("update description preset: %w", err)
	}
	if err := requireChanged(result, "description preset"); err != nil {
		return domain.DescriptionPreset{}, err
	}
	return preset, nil
}

// ListDescriptionPresets lists description presets.
func (store *Store) ListDescriptionPresets(ctx context.Context, includeInactive bool) ([]domain.DescriptionPreset, error) {
	query := `SELECT id, label, active FROM description_presets`
	if !includeInactive {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY label`
	rows, err := store.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list description presets: %w", err)
	}
	defer rows.Close()
	var presets []domain.DescriptionPreset
	for rows.Next() {
		var preset domain.DescriptionPreset
		if err := rows.Scan(&preset.ID, &preset.Label, &preset.Active); err != nil {
			return nil, fmt.Errorf("scan description preset: %w", err)
		}
		presets = append(presets, preset)
	}
	return presets, rows.Err()
}

// SetDescriptionPresetActive changes description availability.
func (store *Store) SetDescriptionPresetActive(ctx context.Context, id int64, active bool) error {
	result, err := store.db.ExecContext(ctx, `UPDATE description_presets SET active = ? WHERE id = ?`, active, id)
	if err != nil {
		return fmt.Errorf("update description preset status: %w", err)
	}
	return requireChanged(result, "description preset")
}

// CreateTimeEntry validates and stores an entry.
func (store *Store) CreateTimeEntry(ctx context.Context, entry domain.TimeEntry) (domain.TimeEntry, error) {
	if err := prepareTimeEntry(&entry); err != nil {
		return domain.TimeEntry{}, err
	}
	entry.CreatedAt = time.Now().UTC()
	result, err := store.db.ExecContext(ctx, `INSERT INTO time_entries(
		start_at, end_at, hours, description, rate_amount, currency, notes,
		description_preset_id, rate_preset_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(entry.StartAt), formatTime(entry.EndAt), entry.Hours, entry.Description,
		entry.RateAmountCents, entry.Currency, entry.Notes, entry.DescriptionPresetID,
		entry.RatePresetID, formatTime(entry.CreatedAt))
	if err != nil {
		return domain.TimeEntry{}, fmt.Errorf("create time entry: %w", err)
	}
	entry.ID, err = result.LastInsertId()
	return entry, err
}

// UpdateTimeEntry validates and updates an entry.
func (store *Store) UpdateTimeEntry(ctx context.Context, entry domain.TimeEntry) (domain.TimeEntry, error) {
	if err := prepareTimeEntry(&entry); err != nil {
		return domain.TimeEntry{}, err
	}
	result, err := store.db.ExecContext(ctx, `UPDATE time_entries SET
		start_at = ?, end_at = ?, hours = ?, description = ?, rate_amount = ?, currency = ?, notes = ?,
		description_preset_id = ?, rate_preset_id = ? WHERE id = ? AND invoice_id IS NULL`,
		formatTime(entry.StartAt), formatTime(entry.EndAt), entry.Hours, entry.Description,
		entry.RateAmountCents, entry.Currency, entry.Notes, entry.DescriptionPresetID, entry.RatePresetID, entry.ID)
	if err != nil {
		return domain.TimeEntry{}, fmt.Errorf("update time entry: %w", err)
	}
	if err := requireChanged(result, "uninvoiced time entry"); err != nil {
		return domain.TimeEntry{}, err
	}
	return entry, nil
}

// DeleteTimeEntry removes an uninvoiced entry.
func (store *Store) DeleteTimeEntry(ctx context.Context, id int64) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ? AND invoice_id IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete time entry: %w", err)
	}
	return requireChanged(result, "uninvoiced time entry")
}

// GetTimeEntry loads one entry for editing.
func (store *Store) GetTimeEntry(ctx context.Context, id int64) (domain.TimeEntry, error) {
	entry, err := scanTimeEntry(store.db.QueryRowContext(ctx, timeEntrySelect+` WHERE id = ?`, id))
	if err != nil {
		return domain.TimeEntry{}, fmt.Errorf("get time entry: %w", err)
	}
	return entry, nil
}

// ListTimeEntries lists entries whose start time is within a half-open range.
func (store *Store) ListTimeEntries(ctx context.Context, from, to time.Time, uninvoicedOnly bool) ([]domain.TimeEntry, error) {
	if !to.After(from) {
		return nil, errors.New("range end must be after range start")
	}
	query := timeEntrySelect + ` WHERE start_at >= ? AND start_at < ?`
	if uninvoicedOnly {
		query += ` AND invoice_id IS NULL`
	}
	query += ` ORDER BY start_at, id`
	rows, err := store.db.QueryContext(ctx, query, formatTime(from), formatTime(to))
	if err != nil {
		return nil, fmt.Errorf("list time entries: %w", err)
	}
	defer rows.Close()
	var entries []domain.TimeEntry
	for rows.Next() {
		entry, err := scanTimeEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// FinalizeInvoice snapshots entries into one invoice transaction.
func (store *Store) FinalizeInvoice(ctx context.Context, draft domain.InvoiceDraft) (domain.Invoice, error) {
	if len(draft.EntryIDs) == 0 {
		return domain.Invoice{}, errors.New("at least one time entry is required")
	}
	if !draft.PeriodEnd.After(draft.PeriodStart) {
		return domain.Invoice{}, errors.New("invoice period end must be after its start")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("begin invoice: %w", err)
	}
	defer tx.Rollback()

	invoice := invoiceFromDraft(draft)
	if strings.TrimSpace(invoice.NumberOverride) == "" {
		var sequence int64
		if err := tx.QueryRowContext(ctx, `UPDATE invoice_sequence SET next_value = next_value + 1 WHERE singleton = 1 RETURNING next_value - 1`).Scan(&sequence); err != nil {
			return domain.Invoice{}, fmt.Errorf("allocate invoice number: %w", err)
		}
		invoice.NumberSequence = &sequence
	}

	for _, id := range draft.EntryIDs {
		entry, err := scanTimeEntry(tx.QueryRowContext(ctx, timeEntrySelect+` WHERE id = ? AND invoice_id IS NULL`, id))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.Invoice{}, fmt.Errorf("time entry %d is missing or already invoiced", id)
			}
			return domain.Invoice{}, err
		}
		if entry.Currency != invoice.Currency {
			return domain.Invoice{}, fmt.Errorf("time entry %d currency %s does not match invoice currency %s", id, entry.Currency, invoice.Currency)
		}
		lineTotal, err := domain.LineTotal(entry.RateAmountCents, entry.Hours)
		if err != nil {
			return domain.Invoice{}, err
		}
		if lineTotal > 0 && invoice.SubtotalCents > math.MaxInt64-lineTotal {
			return domain.Invoice{}, errors.New("invoice subtotal is too large")
		}
		invoice.SubtotalCents += lineTotal
		entryID := entry.ID
		invoice.LineItems = append(invoice.LineItems, domain.InvoiceLineItem{
			SourceTimeEntryID: &entryID, Description: entry.Description, UnitPriceCents: entry.RateAmountCents,
			Units: entry.Hours, LineTotalCents: lineTotal,
		})
	}
	if invoice.AdjustmentCents > 0 && invoice.SubtotalCents > math.MaxInt64-invoice.AdjustmentCents {
		return domain.Invoice{}, errors.New("invoice total is too large")
	}
	invoice.TotalCents = invoice.SubtotalCents + invoice.AdjustmentCents
	invoice.CreatedAt = time.Now().UTC()

	result, err := tx.ExecContext(ctx, `INSERT INTO invoices(
		invoice_number_seq, invoice_number_override, submitted_date, period_start, period_end,
		from_name, from_address, from_email, to_company, to_address, payable_terms, currency,
		notes, adjustment_amount, subtotal_amount, total_amount, pdf_path, logo_path, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		invoice.NumberSequence, invoice.NumberOverride, formatSnapshotTime(invoice.SubmittedDate), formatSnapshotTime(invoice.PeriodStart), formatSnapshotTime(invoice.PeriodEnd),
		invoice.FromName, invoice.FromAddress, invoice.FromEmail, invoice.ToCompany, invoice.ToAddress, invoice.PayableTerms, invoice.Currency,
		invoice.Notes, invoice.AdjustmentCents, invoice.SubtotalCents, invoice.TotalCents, "", invoice.LogoPath, formatTime(invoice.CreatedAt))
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("create invoice: %w", err)
	}
	invoice.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("read invoice id: %w", err)
	}
	for index := range invoice.Contacts {
		invoice.Contacts[index].InvoiceID = invoice.ID
		result, err := tx.ExecContext(ctx, `INSERT INTO invoice_contacts(invoice_id, name, email) VALUES (?, ?, ?)`,
			invoice.ID, invoice.Contacts[index].Name, invoice.Contacts[index].Email)
		if err != nil {
			return domain.Invoice{}, fmt.Errorf("create invoice contact: %w", err)
		}
		invoice.Contacts[index].ID, _ = result.LastInsertId()
	}
	for index := range invoice.LineItems {
		line := &invoice.LineItems[index]
		line.InvoiceID = invoice.ID
		result, err := tx.ExecContext(ctx, `INSERT INTO invoice_line_items(
			invoice_id, source_time_entry_id, description, unit_price, units, line_total
		) VALUES (?, ?, ?, ?, ?, ?)`, invoice.ID, line.SourceTimeEntryID, line.Description, line.UnitPriceCents, line.Units, line.LineTotalCents)
		if err != nil {
			return domain.Invoice{}, fmt.Errorf("create invoice line: %w", err)
		}
		line.ID, _ = result.LastInsertId()
		updated, err := tx.ExecContext(ctx, `UPDATE time_entries SET invoice_id = ? WHERE id = ? AND invoice_id IS NULL`, invoice.ID, *line.SourceTimeEntryID)
		if err != nil {
			return domain.Invoice{}, fmt.Errorf("link time entry: %w", err)
		}
		if err := requireChanged(updated, "uninvoiced time entry"); err != nil {
			return domain.Invoice{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Invoice{}, fmt.Errorf("commit invoice: %w", err)
	}
	return invoice, nil
}

// GetInvoice loads an invoice and its relations.
func (store *Store) GetInvoice(ctx context.Context, id int64) (domain.Invoice, error) {
	invoice, err := scanInvoice(store.db.QueryRowContext(ctx, invoiceSelect+` WHERE id = ?`, id))
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("get invoice: %w", err)
	}
	contacts, err := store.db.QueryContext(ctx, `SELECT id, invoice_id, name, email FROM invoice_contacts WHERE invoice_id = ? ORDER BY id`, id)
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("list invoice contacts: %w", err)
	}
	for contacts.Next() {
		var contact domain.InvoiceContact
		if err := contacts.Scan(&contact.ID, &contact.InvoiceID, &contact.Name, &contact.Email); err != nil {
			contacts.Close()
			return domain.Invoice{}, err
		}
		invoice.Contacts = append(invoice.Contacts, contact)
	}
	if err := contacts.Close(); err != nil {
		return domain.Invoice{}, err
	}
	lines, err := store.db.QueryContext(ctx, `SELECT id, invoice_id, source_time_entry_id, description, unit_price, units, line_total FROM invoice_line_items WHERE invoice_id = ? ORDER BY id`, id)
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("list invoice lines: %w", err)
	}
	defer lines.Close()
	for lines.Next() {
		var line domain.InvoiceLineItem
		var source sql.NullInt64
		if err := lines.Scan(&line.ID, &line.InvoiceID, &source, &line.Description, &line.UnitPriceCents, &line.Units, &line.LineTotalCents); err != nil {
			return domain.Invoice{}, err
		}
		line.SourceTimeEntryID = int64Pointer(source)
		invoice.LineItems = append(invoice.LineItems, line)
	}
	return invoice, lines.Err()
}

// ListInvoices lists finalized invoice summaries newest first.
func (store *Store) ListInvoices(ctx context.Context) ([]domain.Invoice, error) {
	rows, err := store.db.QueryContext(ctx, invoiceSelect+` ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()
	var invoices []domain.Invoice
	for rows.Next() {
		invoice, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, invoice)
	}
	return invoices, rows.Err()
}

// SetInvoicePDFPath records the most recent successful export.
func (store *Store) SetInvoicePDFPath(ctx context.Context, id int64, path string) error {
	result, err := store.db.ExecContext(ctx, `UPDATE invoices SET pdf_path = ? WHERE id = ?`, path, id)
	if err != nil {
		return fmt.Errorf("record PDF path: %w", err)
	}
	return requireChanged(result, "invoice")
}

const timeEntrySelect = `SELECT id, start_at, end_at, hours, description, rate_amount, currency, notes,
	description_preset_id, rate_preset_id, invoice_id, created_at FROM time_entries`

const invoiceSelect = `SELECT id, invoice_number_seq, invoice_number_override, submitted_date, period_start, period_end,
	from_name, from_address, from_email, to_company, to_address, payable_terms, currency, notes,
	adjustment_amount, subtotal_amount, total_amount, pdf_path, logo_path, created_at FROM invoices`

// scanner is shared by query rows and single-row queries.
type scanner interface {
	Scan(dest ...any) error
}

// scanTimeEntry converts persisted timestamp and nullable fields.
func scanTimeEntry(row scanner) (domain.TimeEntry, error) {
	var entry domain.TimeEntry
	var start, end, created string
	var descriptionPreset, ratePreset, invoice sql.NullInt64
	if err := row.Scan(&entry.ID, &start, &end, &entry.Hours, &entry.Description, &entry.RateAmountCents,
		&entry.Currency, &entry.Notes, &descriptionPreset, &ratePreset, &invoice, &created); err != nil {
		return domain.TimeEntry{}, err
	}
	var err error
	if entry.StartAt, err = parseTime(start); err != nil {
		return domain.TimeEntry{}, err
	}
	if entry.EndAt, err = parseTime(end); err != nil {
		return domain.TimeEntry{}, err
	}
	if entry.CreatedAt, err = parseTime(created); err != nil {
		return domain.TimeEntry{}, err
	}
	entry.DescriptionPresetID = int64Pointer(descriptionPreset)
	entry.RatePresetID = int64Pointer(ratePreset)
	entry.InvoiceID = int64Pointer(invoice)
	return entry, nil
}

// scanInvoice converts a persisted invoice row.
func scanInvoice(row scanner) (domain.Invoice, error) {
	var invoice domain.Invoice
	var sequence sql.NullInt64
	var submitted, start, end, created string
	if err := row.Scan(&invoice.ID, &sequence, &invoice.NumberOverride, &submitted, &start, &end,
		&invoice.FromName, &invoice.FromAddress, &invoice.FromEmail, &invoice.ToCompany, &invoice.ToAddress,
		&invoice.PayableTerms, &invoice.Currency, &invoice.Notes, &invoice.AdjustmentCents, &invoice.SubtotalCents,
		&invoice.TotalCents, &invoice.PDFPath, &invoice.LogoPath, &created); err != nil {
		return domain.Invoice{}, err
	}
	var err error
	invoice.NumberSequence = int64Pointer(sequence)
	if invoice.SubmittedDate, err = parseTime(submitted); err != nil {
		return domain.Invoice{}, err
	}
	if invoice.PeriodStart, err = parseTime(start); err != nil {
		return domain.Invoice{}, err
	}
	if invoice.PeriodEnd, err = parseTime(end); err != nil {
		return domain.Invoice{}, err
	}
	if invoice.CreatedAt, err = parseTime(created); err != nil {
		return domain.Invoice{}, err
	}
	return invoice, nil
}

// invoiceFromDraft copies all editable metadata into its immutable snapshot.
func invoiceFromDraft(draft domain.InvoiceDraft) domain.Invoice {
	return domain.Invoice{
		NumberOverride: strings.TrimSpace(draft.NumberOverride), SubmittedDate: draft.SubmittedDate,
		PeriodStart: draft.PeriodStart, PeriodEnd: draft.PeriodEnd,
		FromName: draft.FromName, FromAddress: draft.FromAddress, FromEmail: draft.FromEmail,
		ToCompany: draft.ToCompany, ToAddress: draft.ToAddress, PayableTerms: draft.PayableTerms,
		Currency: draft.Currency, Notes: draft.Notes, AdjustmentCents: draft.AdjustmentCents,
		LogoPath: draft.LogoPath, Contacts: append([]domain.InvoiceContact(nil), draft.Contacts...),
	}
}

// prepareTimeEntry derives hours and normalizes required text fields.
func prepareTimeEntry(entry *domain.TimeEntry) error {
	hours, err := domain.Hours(entry.StartAt, entry.EndAt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(entry.Description) == "" || strings.TrimSpace(entry.Currency) == "" {
		return errors.New("description and currency are required")
	}
	if entry.RateAmountCents < 0 {
		return errors.New("rate must be non-negative")
	}
	entry.Hours = hours
	entry.Description = strings.TrimSpace(entry.Description)
	entry.Currency = strings.TrimSpace(entry.Currency)
	return nil
}

// validateRatePreset checks reusable rate values.
func validateRatePreset(preset domain.RatePreset) error {
	if strings.TrimSpace(preset.Label) == "" || strings.TrimSpace(preset.Currency) == "" {
		return errors.New("rate label and currency are required")
	}
	if preset.AmountCents < 0 {
		return errors.New("rate amount must be non-negative")
	}
	return nil
}

// requireChanged turns missing update targets into a useful error.
func requireChanged(result sql.Result, name string) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%s not found", name)
	}
	return nil
}

// int64Pointer converts nullable database keys.
func int64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copy := value.Int64
	return &copy
}

// formatTime stores timestamps in a sortable UTC representation.
func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

// formatSnapshotTime preserves the local calendar date used on an invoice.
func formatSnapshotTime(value time.Time) string { return value.Format(time.RFC3339Nano) }

// parseTime reads persisted UTC timestamps.
func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time: %w", err)
	}
	return parsed, nil
}
