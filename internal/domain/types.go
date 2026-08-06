package domain

import (
	"strconv"
	"time"
)

// RatePreset is a reusable unit price.
type RatePreset struct {
	ID          int64
	Label       string
	AmountCents int64
	Currency    string
	Active      bool
}

// DescriptionPreset is a reusable line description.
type DescriptionPreset struct {
	ID     int64
	Label  string
	Active bool
}

// TimeEntry records a manually entered work interval.
type TimeEntry struct {
	ID                  int64
	StartAt             time.Time
	EndAt               time.Time
	Hours               float64
	Description         string
	RateAmountCents     int64
	Currency            string
	Notes               string
	DescriptionPresetID *int64
	RatePresetID        *int64
	InvoiceID           *int64
	CreatedAt           time.Time
}

// LineTotalCents returns this entry's derived value.
func (entry TimeEntry) LineTotalCents() int64 {
	total, _ := LineTotal(entry.RateAmountCents, entry.Hours)
	return total
}

// InvoiceContact is a snapshotted point of contact.
type InvoiceContact struct {
	ID        int64
	InvoiceID int64
	Name      string
	Email     string
}

// InvoiceLineItem is an immutable invoice row.
type InvoiceLineItem struct {
	ID                int64
	InvoiceID         int64
	SourceTimeEntryID *int64
	Description       string
	UnitPriceCents    int64
	Units             float64
	LineTotalCents    int64
}

// InvoiceDraft contains selected entries and editable snapshot metadata.
type InvoiceDraft struct {
	EntryIDs        []int64
	NumberOverride  string
	SubmittedDate   time.Time
	PeriodStart     time.Time
	PeriodEnd       time.Time
	FromName        string
	FromAddress     string
	FromEmail       string
	ToCompany       string
	ToAddress       string
	PayableTerms    string
	Currency        string
	Notes           string
	AdjustmentCents int64
	LogoPath        string
	Contacts        []InvoiceContact
}

// Invoice is a finalized, snapshotted invoice.
type Invoice struct {
	ID              int64
	NumberSequence  *int64
	NumberOverride  string
	SubmittedDate   time.Time
	PeriodStart     time.Time
	PeriodEnd       time.Time
	FromName        string
	FromAddress     string
	FromEmail       string
	ToCompany       string
	ToAddress       string
	PayableTerms    string
	Currency        string
	Notes           string
	AdjustmentCents int64
	SubtotalCents   int64
	TotalCents      int64
	PDFPath         string
	LogoPath        string
	CreatedAt       time.Time
	Contacts        []InvoiceContact
	LineItems       []InvoiceLineItem
}

// DisplayNumber returns the manual number or allocated sequence.
func (invoice Invoice) DisplayNumber() string {
	if invoice.NumberOverride != "" {
		return invoice.NumberOverride
	}
	if invoice.NumberSequence == nil {
		return ""
	}
	return strconv.FormatInt(*invoice.NumberSequence, 10)
}
