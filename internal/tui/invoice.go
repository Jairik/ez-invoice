package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// openInvoiceRange starts the guided invoice flow with today's dates.
func (model *Model) openInvoiceRange() {
	model.workspaceArea = workspaceInvoices
	model.push(screenInvoiceRange)
	today := model.now.Format("2006-01-02")
	model.fields = []field{
		{label: "From", value: today, kind: fieldDate},
		{label: "To", value: today, kind: fieldDate},
		{label: "Choose Entries", kind: fieldAction},
	}
	model.invoiceSelected = map[int64]bool{}
}

// continueInvoiceRange loads eligible rows and selects them by default.
func (model *Model) continueInvoiceRange() {
	from, err := time.ParseInLocation("2006-01-02", model.fields[invoiceRangeFromField].value, time.Local)
	if err != nil {
		model.setError(fmt.Errorf("from date: %w", err))
		return
	}
	to, err := time.ParseInLocation("2006-01-02", model.fields[invoiceRangeToField].value, time.Local)
	if err != nil || to.Before(from) {
		model.setError(fmt.Errorf("to date must not be before from date"))
		return
	}
	entries, err := model.application.Store.ListTimeEntries(context.Background(), from, to.AddDate(0, 0, 1), true)
	if err != nil {
		model.setError(err)
		return
	}
	model.invoiceFrom, model.invoiceTo, model.invoiceEntries = from, to, entries
	model.invoiceSelected = make(map[int64]bool, len(entries))
	for _, entry := range entries {
		model.invoiceSelected[entry.ID] = true
	}
	model.push(screenInvoiceEntries)
}

// updateInvoiceEntries toggles exact rows and advances to metadata.
func (model *Model) updateInvoiceEntries(key tea.KeyMsg) {
	model.moveCursor(key, len(model.invoiceEntries)+1)
	if model.cursor < len(model.invoiceEntries) {
		id := model.invoiceEntries[model.cursor].ID
		switch key.Type {
		case tea.KeyLeft:
			model.invoiceSelected[id] = false
		case tea.KeyRight:
			model.invoiceSelected[id] = true
		case tea.KeyEnter:
			model.invoiceSelected[id] = !model.invoiceSelected[id]
		}
		return
	}
	if key.Type != tea.KeyEnter {
		return
	}
	if len(model.selectedInvoiceIDs()) == 0 {
		model.setError(fmt.Errorf("select at least one time entry"))
		return
	}
	model.openInvoiceMetadata()
}

// openInvoiceMetadata loads current configuration defaults into the third step.
func (model *Model) openInvoiceMetadata() {
	model.push(screenInvoiceMetadata)
	recipients := make([]choice, 0, len(model.application.Config().Recipients))
	for index, recipient := range model.application.Config().Recipients {
		label := recipient.CompanyName
		if strings.TrimSpace(label) == "" {
			label = fmt.Sprintf("Recipient %d (not configured)", index+1)
		}
		recipients = append(recipients, choice{label: label, value: label, id: int64(index + 1)})
	}
	model.fields = []field{
		{label: "Submitted", value: model.now.Format("2006-01-02"), kind: fieldDate},
		choiceField("Recipient", recipients, "Not configured"),
		{label: "Invoice Number", placeholder: "Automatic", kind: fieldText},
		{label: "Terms", value: model.application.Config().PayableTerms, kind: fieldText},
		{label: "Notes", value: model.application.Config().Notes, kind: fieldText},
		{label: "Adjustment", value: model.application.Config().DefaultAdjustment, kind: fieldText},
		{label: "Review Invoice", kind: fieldAction},
	}
}

// continueInvoiceMetadata validates configuration and calculates the review totals.
func (model *Model) continueInvoiceMetadata() {
	options, err := model.invoiceOptions()
	if err != nil {
		model.setError(err)
		return
	}
	preview, err := model.application.PreviewInvoice(context.Background(), options)
	if err != nil {
		model.setError(err)
		return
	}
	model.invoicePreview = preview
	model.invoiceMetadata = cloneFields(model.fields)
	model.push(screenInvoiceReview)
	model.cursor = len(model.invoicePreview.Entries)
}

// invoiceOptions translates guided form state into the shared application contract.
func (model *Model) invoiceOptions() (app.InvoiceOptions, error) {
	submitted, err := time.ParseInLocation("2006-01-02", model.fields[invoiceMetadataSubmittedField].value, time.Local)
	if err != nil {
		return app.InvoiceOptions{}, fmt.Errorf("submitted date: %w", err)
	}
	adjustment, err := domain.ParseMoney(model.fields[invoiceMetadataAdjustmentField].value)
	if err != nil {
		return app.InvoiceOptions{}, fmt.Errorf("adjustment: %w", err)
	}
	return app.InvoiceOptions{
		From: model.invoiceFrom, To: model.invoiceTo.AddDate(0, 0, 1), IncludeIDs: model.selectedInvoiceIDs(),
		SubmittedDate: submitted, RecipientIndex: int(model.fields[invoiceMetadataRecipientField].choiceID()) - 1,
		NumberOverride: model.fields[invoiceMetadataNumberField].value, PayableTerms: model.fields[invoiceMetadataTermsField].value,
		Notes: model.fields[invoiceMetadataNotesField].value, AdjustmentCents: &adjustment,
	}, nil
}

// selectedInvoiceIDs returns selected rows in stable list order.
func (model *Model) selectedInvoiceIDs() []int64 {
	ids := make([]int64, 0, len(model.invoiceEntries))
	for _, entry := range model.invoiceEntries {
		if model.invoiceSelected[entry.ID] {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

// updateInvoiceReview finalizes the frozen selection or returns to metadata.
func (model *Model) updateInvoiceReview(key tea.KeyMsg) {
	rowCount := len(model.invoicePreview.Entries)
	model.moveCursor(key, rowCount+2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor < rowCount {
		return
	}
	if model.cursor == rowCount+1 {
		model.back()
		return
	}
	metadata := model.invoiceMetadata
	if len(metadata) <= invoiceMetadataContinueField {
		model.setError(fmt.Errorf("invoice details are no longer available"))
		return
	}
	args := []string{"invoice", "generate", "--from", model.invoiceFrom.Format("2006-01-02"), "--to", model.invoiceTo.Format("2006-01-02")}
	args = append(args, "--include", joinIDs(model.selectedInvoiceIDs()))
	args = append(args,
		"--submitted", metadata[invoiceMetadataSubmittedField].value,
		"--recipient", strconv.FormatInt(metadata[invoiceMetadataRecipientField].choiceID(), 10),
		"--terms", metadata[invoiceMetadataTermsField].value,
		"--notes", metadata[invoiceMetadataNotesField].value,
		"--adjustment", metadata[invoiceMetadataAdjustmentField].value,
	)
	if number := metadata[invoiceMetadataNumberField].value; number != "" {
		args = append(args, "--number", number)
	}
	if !model.runCLI(args) && model.partialInvoice == 0 {
		return
	}
	message := model.status
	model.workspaceArea = workspaceInvoices
	model.screen, model.stack, model.cursor = screenInvoices, []location{{screen: screenHome, cursor: 3}}, 0
	model.loadInvoices()
	model.status = message
}

// joinIDs formats stable row selections for the shared CLI.
func joinIDs(ids []int64) string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = strconv.FormatInt(id, 10)
	}
	return strings.Join(values, ",")
}

// openInvoices loads generated invoice history.
func (model *Model) openInvoices() {
	model.workspaceArea = workspaceInvoices
	model.push(screenInvoices)
	model.loadInvoices()
}

// loadInvoices refreshes finalized invoice rows.
func (model *Model) loadInvoices() {
	invoices, err := model.application.Store.ListInvoices(context.Background())
	if err != nil {
		model.setError(err)
		return
	}
	model.invoices = invoices
}

// updateInvoices selects a stored invoice summary.
func (model *Model) updateInvoices(key tea.KeyMsg) {
	model.moveCursor(key, len(model.invoices))
	if key.Type == tea.KeyEnter && len(model.invoices) > 0 {
		model.selectedInvoice = model.invoices[model.cursor].ID
		model.actionMode, model.actionCursor = true, 0
	}
}

// updateInvoiceActions exports a selected immutable snapshot.
func (model *Model) updateInvoiceActions(key tea.KeyMsg) {
	model.updateConfirmStrip(key, func() {
		model.runCLI([]string{"invoice", "export", strconv.FormatInt(model.selectedInvoice, 10)})
	})
}

// invoiceEntriesView renders checkbox rows and the guided Continue action.
func (model Model) invoiceEntriesView() string {
	lines := []string{accentStyle.Render("Choose time entries  ·  Dates › Entries › Details › Review"), mutedStyle.Render("All eligible rows start selected."), ""}
	selected := model.cursor
	if selected == len(model.invoiceEntries) {
		selected--
	}
	start, end := model.listWindow(len(model.invoiceEntries), selected, 16)
	for index := start; index < end; index++ {
		entry := model.invoiceEntries[index]
		mark := " "
		if model.invoiceSelected[entry.ID] {
			mark = "✓"
		}
		line := fmt.Sprintf("[%s] %-10s  %-28s %s %s", mark, entry.StartAt.Local().Format("2006-01-02"), truncate(entry.Description, 28), entry.Currency, domain.FormatMoney(entry.LineTotalCents()))
		if model.cursor == index {
			line = selectedStyle.Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if len(model.invoiceEntries) == 0 {
		lines = append(lines, mutedStyle.Render("  No uninvoiced entries in this range."))
	}
	continueLine := "  Review Details"
	if model.cursor == len(model.invoiceEntries) {
		continueLine = selectedStyle.Render("› Review Details")
	}
	lines = append(lines, "", continueLine)
	return strings.Join(lines, "\n")
}

// invoiceReviewView renders calculated totals and the final action.
func (model Model) invoiceReviewView() string {
	lines := []string{accentStyle.Render("Review invoice  ·  Dates › Entries › Details › Review"), ""}
	selected := model.cursor
	if selected >= len(model.invoicePreview.Entries) {
		selected = len(model.invoicePreview.Entries) - 1
	}
	start, end := model.listWindow(len(model.invoicePreview.Entries), selected, 20)
	for index := start; index < end; index++ {
		entry := model.invoicePreview.Entries[index]
		line := fmt.Sprintf("%-32s %5.2f × %s = %s", truncate(entry.Description, 32), entry.Hours,
			domain.FormatMoney(entry.RateAmountCents), domain.FormatMoney(entry.LineTotalCents()))
		if model.cursor == index {
			line = selectedStyle.Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", fmt.Sprintf("  Subtotal%38s %s", "", domain.FormatMoney(model.invoicePreview.SubtotalCents)),
		fmt.Sprintf("  Adjustment%36s %s", "", domain.FormatMoney(model.invoicePreview.AdjustmentCents)),
		valueStyle.Render(fmt.Sprintf("  Total%41s %s %s", "", model.application.Config().Currency, domain.FormatMoney(model.invoicePreview.TotalCents))), "")
	buttons := []string{"Finalize & Export PDF", "Back"}
	for index, label := range buttons {
		line := "  " + label
		if model.cursor == len(model.invoicePreview.Entries)+index {
			line = selectedStyle.Render("› " + label)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// invoicesView renders finalized history.
func (model Model) invoicesView() string {
	lines := []string{accentStyle.Render("Invoices"), "", mutedStyle.Render("    NUMBER       SUBMITTED    PERIOD                    TOTAL")}
	if len(model.invoices) == 0 {
		lines = append(lines, mutedStyle.Render("    No invoices yet."))
	}
	start, end := model.listWindow(len(model.invoices), model.cursor, 14)
	for index := start; index < end; index++ {
		invoice := model.invoices[index]
		line := fmt.Sprintf("%-12s %-12s %-10s – %-10s  %s %s", invoice.DisplayNumber(), invoice.SubmittedDate.Format("2006-01-02"),
			invoice.PeriodStart.Format("2006-01-02"), invoice.PeriodEnd.Format("2006-01-02"), invoice.Currency, domain.FormatMoney(invoice.TotalCents))
		if model.cursor == index {
			line = selectedStyle.Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if actions := model.inlineActionView(); actions != "" {
		lines = append(lines, actions)
	}
	return strings.Join(lines, "\n")
}

// invoiceActionsView renders summary and export action for a selected invoice.
func (model Model) invoiceActionsView() string {
	var selected domain.Invoice
	for _, invoice := range model.invoices {
		if invoice.ID == model.selectedInvoice {
			selected = invoice
			break
		}
	}
	title := fmt.Sprintf("Invoice %s", selected.DisplayNumber())
	details := []string{
		fmt.Sprintf("Submitted: %s", selected.SubmittedDate.Format("2006-01-02")),
		fmt.Sprintf("Total: %s %s", selected.Currency, domain.FormatMoney(selected.TotalCents)),
		fmt.Sprintf("PDF: %s", selected.PDFPath), "",
	}
	return strings.Join(append([]string{accentStyle.Render(title), ""}, append(details, model.actionLines([]string{"Export PDF", "Back"})...)...), "\n")
}
