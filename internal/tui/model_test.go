package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// TestHomeNavigationOpensAndClosesScreens catches broken arrow routing and back navigation.
func TestHomeNavigationOpensAndClosesScreens(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))

	model = press(model, tea.KeyDown)
	model = press(model, tea.KeyEnter)
	if model.screen != screenTimeEntries {
		t.Fatalf("Down then Enter opened screen %v, want time entries", model.screen)
	}
	model = press(model, tea.KeyEsc)
	if model.screen != screenHome || model.cursor != 1 {
		t.Fatalf("Escape returned model to screen=%v cursor=%d, want home cursor 1", model.screen, model.cursor)
	}
}

// TestAddTimeDefaultsAndArrowAdjustments catches regressions in today and preset defaults.
func TestAddTimeDefaultsAndArrowAdjustments(t *testing.T) {
	application := openTUITestApp(t)
	ctx := context.Background()
	description, err := application.Store.CreateDescriptionPreset(ctx, domain.DescriptionPreset{Label: "Development", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	firstRate, err := application.Store.CreateRatePreset(ctx, domain.RatePreset{Label: "Standard", AmountCents: 12_500, Currency: "USD", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	secondRate, err := application.Store.CreateRatePreset(ctx, domain.RatePreset{Label: "Urgent", AmountCents: 17_500, Currency: "USD", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)

	if model.screen != screenTimeForm || model.fields[timeDateField].value != "2026-08-06" {
		t.Fatalf("Add Time defaults = screen %v fields %+v", model.screen, model.fields)
	}
	if model.fields[timeDescriptionField].choiceID() != description.ID || model.fields[timeRateField].choiceID() != firstRate.ID {
		t.Fatalf("preset defaults = description %d rate %d", model.fields[timeDescriptionField].choiceID(), model.fields[timeRateField].choiceID())
	}

	model.cursor = timeDateField
	model = press(model, tea.KeyUp)
	if model.fields[timeDateField].value != "2026-08-07" {
		t.Fatalf("Up adjusted date to %q", model.fields[timeDateField].value)
	}
	model.cursor = timeRateField
	model = press(model, tea.KeyUp)
	if model.fields[timeRateField].choiceID() != secondRate.ID {
		t.Fatalf("Up selected rate %d, want %d", model.fields[timeRateField].choiceID(), secondRate.ID)
	}
	model.fields[timeStartField].value = "09:00"
	model.cursor = timeStartField
	model = press(model, tea.KeyUp)
	if model.fields[timeStartField].value != "09:15" {
		t.Fatalf("Up adjusted start time to %q", model.fields[timeStartField].value)
	}
	model.cursor = timeStartPeriodField
	model = press(model, tea.KeyUp)
	if model.fields[timeStartPeriodField].value != "PM" {
		t.Fatalf("Up adjusted start period to %q", model.fields[timeStartPeriodField].value)
	}
}

// TestAddTimeFormPersistsEntry catches a form that renders but does not call real storage behavior.
func TestAddTimeFormPersistsEntry(t *testing.T) {
	application := openTUITestApp(t)
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)
	model.fields[timeStartField].value = "09:00"
	model.fields[timeEndField].value = "10:30"
	model.fields[timeDescriptionField].value = "Client work"
	model.fields[timeRateField].value = "100.00"
	model.cursor = timeSaveField

	model = press(model, tea.KeyEnter)
	if model.screen != screenTimeConfirm || !strings.Contains(model.View(), "1h 30m") || !strings.Contains(model.View(), "Aug 6, 2026 at 9:00 AM") {
		t.Fatalf("confirmation screen=%v view=%q", model.screen, model.View())
	}
	model = press(model, tea.KeyEnter)
	entries, err := application.Store.ListTimeEntries(context.Background(),
		time.Date(2026, 8, 6, 0, 0, 0, 0, time.Local), time.Date(2026, 8, 7, 0, 0, 0, 0, time.Local), false)
	if err != nil {
		t.Fatal(err)
	}
	if model.screen != screenTimeEntries || len(entries) != 1 || entries[0].Description != "Client work" || entries[0].RateAmountCents != 10_000 {
		t.Fatalf("saved model=%+v entries=%+v", model, entries)
	}
}

// TestInvoiceWorkflowSelectsExactRows catches broken guided transitions and row toggling.
func TestInvoiceWorkflowSelectsExactRows(t *testing.T) {
	application := openTUITestApp(t)
	application.Config.Sender = config.Sender{FullName: "Ada Lovelace", Address: "1 Computing Ln", Email: "ada@example.com"}
	application.Config.Recipients[0] = config.Recipient{CompanyName: "Analytical Engines", Address: "2 Difference Rd"}
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 6, 9, 0, 0, 0, time.Local)
	for index := 0; index < 2; index++ {
		if _, err := application.Store.CreateTimeEntry(context.Background(), domain.TimeEntry{
			StartAt: start.Add(time.Duration(index) * 2 * time.Hour), EndAt: start.Add(time.Duration(index)*2*time.Hour + time.Hour),
			Description: "Development", RateAmountCents: 10_000, Currency: "USD",
		}); err != nil {
			t.Fatal(err)
		}
	}
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.cursor = 2
	model = press(model, tea.KeyEnter)
	model.cursor = invoiceRangeContinueField
	model = press(model, tea.KeyEnter)
	if model.screen != screenInvoiceEntries || len(model.invoiceEntries) != 2 {
		t.Fatalf("range step produced screen=%v entries=%d status=%q", model.screen, len(model.invoiceEntries), model.status)
	}
	model.cursor = 0
	model = press(model, tea.KeyLeft)
	if model.invoiceSelected[model.invoiceEntries[0].ID] {
		t.Fatal("Left did not deselect the highlighted invoice row")
	}
	model.cursor = len(model.invoiceEntries)
	model = press(model, tea.KeyEnter)
	if model.screen != screenInvoiceMetadata {
		t.Fatalf("Continue opened screen %v, want invoice metadata", model.screen)
	}
	model.cursor = invoiceMetadataContinueField
	model = press(model, tea.KeyEnter)
	if model.screen != screenInvoiceReview || len(model.invoicePreview.Entries) != 1 {
		t.Fatalf("review screen=%v preview=%+v status=%q", model.screen, model.invoicePreview, model.status)
	}
	badLogo := filepath.Join(t.TempDir(), "not-an-image.txt")
	if err := os.WriteFile(badLogo, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	application.Config.LogoPath = badLogo
	if err := config.Save(application.Paths.ConfigFile, application.Config); err != nil {
		t.Fatal(err)
	}
	model.cursor = len(model.invoicePreview.Entries)
	model = press(model, tea.KeyEnter)
	invoices, err := application.Store.ListInvoices(context.Background())
	if err != nil || len(invoices) != 1 {
		t.Fatalf("finalized invoices=%+v err=%v status=%q", invoices, err, model.status)
	}
	invoice, err := application.Store.GetInvoice(context.Background(), invoices[0].ID)
	if err != nil || len(invoice.LineItems) != 1 || invoice.LineItems[0].SourceTimeEntryID == nil || *invoice.LineItems[0].SourceTimeEntryID != 2 {
		t.Fatalf("finalized exact selection invoice=%+v err=%v", invoice, err)
	}
	if model.screen != screenInvoices || !strings.Contains(model.status, "finalized") || !strings.Contains(model.status, "PDF") {
		t.Fatalf("partial export result screen=%v status=%q", model.screen, model.status)
	}
}

// TestPresetMenuCreatesRate catches preset screens that only display data without exposing commands.
func TestPresetMenuCreatesRate(t *testing.T) {
	application := openTUITestApp(t)
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.cursor = 4
	model = press(model, tea.KeyEnter)
	model = press(model, tea.KeyEnter)
	model = press(model, tea.KeyEnter)
	if model.screen != screenRateForm {
		t.Fatalf("Add Rate opened screen %v", model.screen)
	}
	model.fields[rateLabelField].value = "Standard"
	model.fields[rateAmountField].value = "125.00"
	model.fields[rateCurrencyField].value = "USD"
	model.cursor = rateSaveField
	model = press(model, tea.KeyEnter)

	presets, err := application.Store.ListRatePresets(context.Background(), false)
	if err != nil || len(presets) != 1 || presets[0].AmountCents != 12_500 {
		t.Fatalf("created rate presets=%+v err=%v status=%q", presets, err, model.status)
	}
	if model.screen != screenRates {
		t.Fatalf("save returned to screen %v, want rates", model.screen)
	}
}

// TestSettingsSenderFormPersistsConfig catches settings that appear editable but do not save.
func TestSettingsSenderFormPersistsConfig(t *testing.T) {
	application := openTUITestApp(t)
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.cursor = 5
	model = press(model, tea.KeyEnter)
	model = press(model, tea.KeyEnter)
	if model.screen != screenSettingsForm {
		t.Fatalf("Sender settings opened screen %v", model.screen)
	}
	model.fields[senderNameField].value = "Ada Lovelace"
	model.fields[senderAddressField].value = "1 Computing Lane"
	model.fields[senderEmailField].value = "ada@example.com"
	model.cursor = senderSaveField
	model = press(model, tea.KeyEnter)

	loaded, err := config.Load(application.Paths.ConfigFile)
	if err != nil || loaded.Sender.FullName != "Ada Lovelace" || loaded.Sender.Email != "ada@example.com" {
		t.Fatalf("saved config=%+v err=%v status=%q", loaded, err, model.status)
	}
}

// TestEditingModeCancelsWithoutLosingTheOriginal catches Escape leaking partial text edits.
func TestEditingModeCancelsWithoutLosingTheOriginal(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)
	model.cursor = timeDateField
	model = press(model, tea.KeyEnter)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("oops")})
	model = updated.(Model)
	model = press(model, tea.KeyEsc)
	if model.editing || model.fields[timeDateField].value != "2026-08-06" {
		t.Fatalf("cancelled editor left editing=%t date=%q", model.editing, model.fields[timeDateField].value)
	}
}

// TestEditingModeAcceptsSpaces catches multi-word names and descriptions losing whitespace.
func TestEditingModeAcceptsSpaces(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)
	model.cursor = timeDescriptionField
	model = press(model, tea.KeyEnter)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Client")})
	model = updated.(Model)
	model = press(model, tea.KeySpace)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("work")})
	model = updated.(Model)
	model = press(model, tea.KeyEnter)
	if model.fields[timeDescriptionField].value != "Client work" {
		t.Fatalf("edited description = %q", model.fields[timeDescriptionField].value)
	}
}

// TestLongTimeEntryListStaysWithinTerminal catches selected rows overflowing the alternate screen.
func TestLongTimeEntryListStaysWithinTerminal(t *testing.T) {
	application := openTUITestApp(t)
	start := time.Date(2026, 8, 6, 9, 0, 0, 0, time.Local)
	for index := 0; index < 20; index++ {
		if _, err := application.Store.CreateTimeEntry(context.Background(), domain.TimeEntry{
			StartAt: start.Add(time.Duration(index) * time.Minute), EndAt: start.Add(time.Duration(index+1) * time.Minute),
			Description: "Entry " + strconv.Itoa(index), RateAmountCents: 10_000, Currency: "USD",
		}); err != nil {
			t.Fatal(err)
		}
	}
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.height, model.width = 24, 90
	model.openTimeEntries()
	model.cursor = len(model.entries) + 2
	view := model.View()
	if lipgloss.Height(view) > model.height || !strings.Contains(view, "Entry 19") || strings.Contains(view, "Entry 0 ") {
		t.Fatalf("long list height=%d view=%q", lipgloss.Height(view), view)
	}
}

// TestTimeEntryRowFitsStandardTerminal catches totals wrapping onto a second table line.
func TestTimeEntryRowFitsStandardTerminal(t *testing.T) {
	application := openTUITestApp(t)
	start := time.Date(2026, 8, 6, 9, 0, 0, 0, time.Local)
	if _, err := application.Store.CreateTimeEntry(context.Background(), domain.TimeEntry{
		StartAt: start, EndAt: start.Add(90 * time.Minute), Description: "Client work", RateAmountCents: 12_500, Currency: "USD",
	}); err != nil {
		t.Fatal(err)
	}
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.width, model.height = 80, 24
	model.openTimeEntries()
	model.cursor = 3
	if view := model.View(); !strings.Contains(view, "USD 187.50") {
		t.Fatalf("time-entry total wrapped or disappeared: %q", view)
	}
}

// TestEditingCustomRatePreservesCurrency catches unrelated edits rewriting historical currency.
func TestEditingCustomRatePreservesCurrency(t *testing.T) {
	application := openTUITestApp(t)
	start := time.Date(2026, 8, 6, 9, 0, 0, 0, time.Local)
	entry, err := application.Store.CreateTimeEntry(context.Background(), domain.TimeEntry{
		StartAt: start, EndAt: start.Add(time.Hour), Description: "Client work", RateAmountCents: 10_000, Currency: "EUR",
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.openTimeForm(&entry)
	model.fields[timeNotesField].value = "Updated"
	model.cursor = timeSaveField
	model = press(model, tea.KeyEnter)
	model = press(model, tea.KeyEnter)
	updated, err := application.Store.GetTimeEntry(context.Background(), entry.ID)
	if err != nil || updated.Currency != "EUR" {
		t.Fatalf("updated entry=%+v err=%v status=%q", updated, err, model.status)
	}
}

// TestEditingOvernightEntryPreservesEndDate catches end times collapsing onto the start day.
func TestEditingOvernightEntryPreservesEndDate(t *testing.T) {
	application := openTUITestApp(t)
	start := time.Date(2026, 8, 6, 23, 30, 0, 0, time.Local)
	entry, err := application.Store.CreateTimeEntry(context.Background(), domain.TimeEntry{
		StartAt: start, EndAt: start.Add(time.Hour), Description: "Deployment", RateAmountCents: 10_000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newAt(application, time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.openTimeForm(&entry)
	if model.fields[timeEndDateField].value != "2026-08-07" {
		t.Fatalf("overnight end date default = %q", model.fields[timeEndDateField].value)
	}
	model.fields[timeNotesField].value = "Updated"
	model.cursor = timeSaveField
	model = press(model, tea.KeyEnter)
	model = press(model, tea.KeyEnter)
	updated, err := application.Store.GetTimeEntry(context.Background(), entry.ID)
	if err != nil || !updated.EndAt.Equal(entry.EndAt) {
		t.Fatalf("updated overnight entry=%+v err=%v status=%q", updated, err, model.status)
	}
}

// TestTimeEntryDirectTypingAddsColon verifies keyboard entry accepts digits with or without a typed separator.
func TestTimeEntryDirectTypingAddsColon(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)
	model.cursor = timeStartField
	model = press(model, tea.KeyEnter)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("09")})
	model = updated.(Model)
	if model.fields[timeStartField].value != "09:" {
		t.Fatalf("automatic colon value = %q", model.fields[timeStartField].value)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":30")})
	model = updated.(Model)
	model = press(model, tea.KeyEnter)
	if model.fields[timeStartField].value != "09:30" {
		t.Fatalf("normalized typed time = %q", model.fields[timeStartField].value)
	}

	model.fields[timeEndField].value = ""
	model.cursor = timeEndField
	model = press(model, tea.KeyEnter)
	for _, digit := range "930" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{digit}})
		model = updated.(Model)
	}
	model = press(model, tea.KeyEnter)
	if model.fields[timeEndField].value != "09:30" {
		t.Fatalf("single-digit hour time = %q", model.fields[timeEndField].value)
	}
}

// TestTimeEntryShowsDurationAndRejectsEarlierEnd verifies live totals and interval validation.
func TestTimeEntryShowsDurationAndRejectsEarlierEnd(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)
	model.fields[timeStartField].value = "10:00"
	model.fields[timeEndField].value = "09:45"
	model.cursor = timeSaveField
	model = press(model, tea.KeyEnter)
	if model.screen != screenTimeForm || !model.statusError || !strings.Contains(model.status, "after start") {
		t.Fatalf("invalid interval screen=%v status=%q", model.screen, model.status)
	}
	model.fields[timeEndField].value = "11:30"
	if view := model.View(); !strings.Contains(view, "1h 30m") {
		t.Fatalf("live duration missing from view: %q", view)
	}
	model = press(model, tea.KeyEnter)
	if model.screen != screenTimeConfirm || !strings.Contains(model.View(), "Total Time") {
		t.Fatalf("valid interval did not open confirmation: screen=%v view=%q", model.screen, model.View())
	}
}

// TestTimeFormUsesArrowOnlyNavigation verifies horizontal movement and vertical value changes.
func TestTimeFormUsesArrowOnlyNavigation(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 7, 0, 0, time.Local))
	model = press(model, tea.KeyEnter)
	model = press(model, tea.KeyRight)
	if model.cursor != timeStartField {
		t.Fatalf("Right selected field %d, want start time", model.cursor)
	}
	model = press(model, tea.KeyUp)
	if model.fields[timeStartField].value != "12:15" || model.fields[timeStartPeriodField].value != "PM" {
		t.Fatalf("Up initialized start to %q %q", model.fields[timeStartField].value, model.fields[timeStartPeriodField].value)
	}
	model = press(model, tea.KeyDown)
	if model.fields[timeStartField].value != "12:00" {
		t.Fatalf("Down adjusted start to %q", model.fields[timeStartField].value)
	}
	model.fields[timeStartField].value = "11:45"
	model.fields[timeStartPeriodField] = periodField("Start AM/PM", "AM")
	model = press(model, tea.KeyUp)
	if model.fields[timeStartField].value != "12:00" || model.fields[timeStartPeriodField].value != "PM" {
		t.Fatalf("noon rollover produced %q %q", model.fields[timeStartField].value, model.fields[timeStartPeriodField].value)
	}
}

// TestTimeFormFitsStandardTerminal verifies the duration line does not overflow a common viewport.
func TestTimeFormFitsStandardTerminal(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.width, model.height = 80, 24
	model = press(model, tea.KeyEnter)
	if view := model.View(); lipgloss.Height(view) > model.height {
		t.Fatalf("time form height=%d exceeds terminal height=%d: %q", lipgloss.Height(view), model.height, view)
	}
}

// TestLongInvoiceReviewKeepsTotalsAndActionsVisible catches rows pushing finalization off-screen.
func TestLongInvoiceReviewKeepsTotalsAndActionsVisible(t *testing.T) {
	model := newAt(openTUITestApp(t), time.Date(2026, 8, 6, 12, 0, 0, 0, time.Local))
	model.screen, model.width, model.height = screenInvoiceReview, 90, 24
	for index := 0; index < 20; index++ {
		model.invoicePreview.Entries = append(model.invoicePreview.Entries, domain.TimeEntry{Description: "Review row " + strconv.Itoa(index), Hours: 1, RateAmountCents: 10_000})
	}
	model.cursor = len(model.invoicePreview.Entries)
	view := model.View()
	if lipgloss.Height(view) > model.height || !strings.Contains(view, "Finalize & Export PDF") || !strings.Contains(view, "Total") || !strings.Contains(view, "Review row 19") {
		t.Fatalf("long review height=%d view=%q", lipgloss.Height(view), view)
	}
	model = press(model, tea.KeyUp)
	if model.cursor != len(model.invoicePreview.Entries)-1 {
		t.Fatalf("Up selected review cursor %d", model.cursor)
	}
}

// TestSmallTerminalFallback catches layouts that render unusable clipped panels.
func TestSmallTerminalFallback(t *testing.T) {
	model := New(openTUITestApp(t))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	view := updated.(Model).View()
	if !strings.Contains(view, "terminal is too small") {
		t.Fatalf("small terminal view = %q", view)
	}
}

// press applies one navigation key to a model.
func press(model Model, key tea.KeyType) Model {
	updated, _ := model.Update(tea.KeyMsg{Type: key})
	return updated.(Model)
}

// openTUITestApp creates isolated state for model tests.
func openTUITestApp(t *testing.T) *app.App {
	t.Helper()
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatalf("app.Open returned an error: %v", err)
	}
	t.Cleanup(func() { application.Close() })
	return application
}
