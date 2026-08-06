// Package tui provides the menu-driven Bubble Tea interface.
package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/cli"
	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

type screen int

const (
	screenHome screen = iota
	screenTimeForm
	screenTimeConfirm
	screenTimeEntries
	screenTimeEntryActions
	screenConfirmTimeDelete
	screenInvoiceRange
	screenInvoiceEntries
	screenInvoiceMetadata
	screenInvoiceReview
	screenInvoices
	screenInvoiceActions
	screenPresets
	screenRates
	screenRateForm
	screenRateActions
	screenConfirmRateToggle
	screenDescriptions
	screenDescriptionForm
	screenDescriptionActions
	screenConfirmDescriptionToggle
	screenSettings
	screenSettingsForm
	screenRecipients
	screenRecipientForm
	screenRecipientActions
	screenConfirmRecipientDelete
	screenContacts
	screenContactForm
	screenContactActions
	screenConfirmContactDelete
)

const (
	workspaceOverview = iota
	workspaceTime
	workspaceInvoices
	workspacePresets
	workspaceSettings
)

const (
	timeDateField = iota
	timeStartField
	timeStartPeriodField
	timeEndDateField
	timeEndField
	timeEndPeriodField
	timeDescriptionField
	timeRateField
	timeNotesField
	timeSaveField
)

const (
	invoiceRangeFromField = iota
	invoiceRangeToField
	invoiceRangeContinueField
)

const (
	invoiceMetadataSubmittedField = iota
	invoiceMetadataRecipientField
	invoiceMetadataNumberField
	invoiceMetadataTermsField
	invoiceMetadataNotesField
	invoiceMetadataAdjustmentField
	invoiceMetadataContinueField
)

const (
	rateLabelField = iota
	rateAmountField
	rateCurrencyField
	rateSaveField
)

const (
	descriptionLabelField = iota
	descriptionSaveField
)

const (
	senderNameField = iota
	senderAddressField
	senderEmailField
	senderSaveField
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldDate
	fieldTime
	fieldChoice
	fieldAction
)

type choice struct {
	label string
	value string
	id    int64
}

type field struct {
	label       string
	value       string
	placeholder string
	kind        fieldKind
	choices     []choice
	choiceAt    int
}

// choiceID returns the selected database identifier or zero for Custom.
func (item field) choiceID() int64 {
	if item.kind != fieldChoice || item.choiceAt < 0 || item.choiceAt >= len(item.choices) {
		return 0
	}
	return item.choices[item.choiceAt].id
}

// displayValue returns the readable choice label or direct field value.
func (item field) displayValue() string {
	if item.kind == fieldChoice && item.choiceAt >= 0 && item.choiceAt < len(item.choices) && item.choices[item.choiceAt].id != 0 {
		return item.choices[item.choiceAt].label
	}
	if item.value == "" {
		return item.placeholder
	}
	return item.value
}

type location struct {
	screen screen
	cursor int
	fields []field
}

var (
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Bold(true).Padding(0, 1)
	valueStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	panelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(1, 2)
)

// Model is the terminal UI state.
type Model struct {
	application *app.App
	screen      screen
	stack       []location
	cursor      int
	fields      []field
	editing     bool
	editAt      int
	editBefore  string
	width       int
	height      int
	status      string
	statusError bool
	now         time.Time
	// workspaceArea tracks the active top-level workspace.
	workspaceArea int
	actionMode    bool
	actionCursor  int

	entries         []domain.TimeEntry
	overviewEntries []domain.TimeEntry
	selectedEntryID int64
	timeEntryID     int64
	timeCurrency    string
	timeStartAt     time.Time
	timeEndAt       time.Time
	invoices        []domain.Invoice
	selectedInvoice int64
	partialInvoice  int64

	invoiceEntries  []domain.TimeEntry
	invoiceSelected map[int64]bool
	invoiceFrom     time.Time
	invoiceTo       time.Time
	invoicePreview  app.InvoicePreview
	invoiceMetadata []field

	selectedRate        int64
	selectedDescription int64
	selectedConfigIndex int
	settingsDefaults    bool
}

// New creates a terminal UI model.
func New(application *app.App) Model { return newAt(application, time.Now()) }

// newAt creates deterministic initial state for tests and the real current time for New.
func newAt(application *app.App, now time.Time) Model {
	model := Model{application: application, screen: screenHome, now: now, workspaceArea: workspaceOverview, invoiceSelected: map[int64]bool{}}
	model.loadOverview()
	return model
}

// Run starts the full-screen terminal interface.
func Run(application *app.App) error {
	_, err := tea.NewProgram(New(application), tea.WithAltScreen()).Run()
	return err
}

// Init starts without an asynchronous command.
func (model Model) Init() tea.Cmd { return nil }

// Update handles terminal input.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		return model, nil
	case tea.KeyMsg:
		if message.Type == tea.KeyCtrlC {
			return model, tea.Quit
		}
		if model.editing {
			model.updateEditor(message)
			return model, nil
		}
		if message.Type == tea.KeyEsc {
			if model.actionMode {
				model.actionMode = false
				return model, nil
			}
			model.back()
			return model, nil
		}
		if model.actionMode {
			model.updateInlineAction(message)
			return model, nil
		}
		if model.handleWorkspaceNavigation(message) {
			return model, nil
		}
		return model.updateNavigation(message)
	}
	return model, nil
}

// updateNavigation routes keys to the active screen.
func (model Model) updateNavigation(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch model.screen {
	case screenHome:
		return model.updateHome(key)
	case screenTimeForm:
		model.updateForm(key, model.submitTimeForm)
	case screenTimeConfirm:
		model.updateTimeConfirmation(key)
	case screenTimeEntries:
		model.updateTimeEntries(key)
	case screenTimeEntryActions:
		model.updateTimeEntryActions(key)
	case screenConfirmTimeDelete:
		model.updateTimeDeleteConfirmation(key)
	case screenInvoiceRange:
		model.updateForm(key, model.continueInvoiceRange)
	case screenInvoiceEntries:
		model.updateInvoiceEntries(key)
	case screenInvoiceMetadata:
		model.updateForm(key, model.continueInvoiceMetadata)
	case screenInvoiceReview:
		model.updateInvoiceReview(key)
	case screenInvoices:
		model.updateInvoices(key)
	case screenInvoiceActions:
		model.updateInvoiceActions(key)
	case screenPresets:
		model.updateSimpleMenu(key, 3, model.openPresetSelection)
	case screenRates:
		model.updateRates(key)
	case screenRateForm:
		model.updateForm(key, model.saveRate)
	case screenRateActions:
		model.updateRateActions(key)
	case screenConfirmRateToggle:
		model.updateRateToggleConfirmation(key)
	case screenDescriptions:
		model.updateDescriptions(key)
	case screenDescriptionForm:
		model.updateForm(key, model.saveDescription)
	case screenDescriptionActions:
		model.updateDescriptionActions(key)
	case screenConfirmDescriptionToggle:
		model.updateDescriptionToggleConfirmation(key)
	case screenSettings:
		model.updateSettings(key)
	case screenSettingsForm:
		model.updateForm(key, model.saveSettingsForm)
	case screenRecipients:
		model.updateRecipients(key)
	case screenRecipientForm:
		model.updateForm(key, model.saveRecipient)
	case screenRecipientActions:
		model.updateRecipientActions(key)
	case screenConfirmRecipientDelete:
		model.updateRecipientDeleteConfirmation(key)
	case screenContacts:
		model.updateContacts(key)
	case screenContactForm:
		model.updateForm(key, model.saveContact)
	case screenContactActions:
		model.updateContactActions(key)
	case screenConfirmContactDelete:
		model.updateContactDeleteConfirmation(key)
	}
	return model, nil
}

// inlineActionLabels returns the actions available for the selected list row.
func (model Model) inlineActionLabels() []string {
	switch model.screen {
	case screenTimeEntries:
		return []string{"Edit", "Delete", "Close"}
	case screenInvoices:
		return []string{"Export PDF", "Close"}
	case screenRates, screenDescriptions:
		return []string{"Edit", "Disable / Restore", "Close"}
	case screenRecipients, screenContacts:
		return []string{"Edit", "Delete", "Close"}
	default:
		return nil
	}
}

// moveActionCursor moves through a selected row's contextual actions.
func (model *Model) moveActionCursor(key tea.KeyMsg, count int) {
	if count == 0 {
		model.actionCursor = 0
		return
	}
	direction := 0
	if key.Type == tea.KeyLeft || key.Type == tea.KeyUp {
		direction = -1
	}
	if key.Type == tea.KeyRight || key.Type == tea.KeyDown {
		direction = 1
	}
	model.actionCursor = (model.actionCursor + direction + count) % count
}

// updateInlineAction handles arrow navigation and activation for list actions.
func (model *Model) updateInlineAction(key tea.KeyMsg) {
	labels := model.inlineActionLabels()
	model.moveActionCursor(key, len(labels))
	if key.Type == tea.KeyEnter {
		model.activateInlineAction()
	}
}

// activateInlineAction runs the selected contextual action without a command menu.
func (model *Model) activateInlineAction() {
	switch model.screen {
	case screenTimeEntries:
		switch model.actionCursor {
		case 0:
			entry, err := model.application.Store.GetTimeEntry(context.Background(), model.selectedEntryID)
			if err != nil {
				model.setError(err)
				return
			}
			model.actionMode = false
			model.openTimeForm(&entry)
		case 1:
			model.actionMode = false
			model.push(screenConfirmTimeDelete)
		case 2:
			model.actionMode = false
		}
	case screenInvoices:
		if model.actionCursor == 0 {
			if model.runCLI([]string{"invoice", "export", strconv.FormatInt(model.selectedInvoice, 10)}) {
				model.actionMode = false
				model.loadInvoices()
			}
			return
		}
		model.actionMode = false
	case screenRates:
		model.activatePresetAction(true)
	case screenDescriptions:
		model.activatePresetAction(false)
	case screenRecipients:
		model.activateConfigAction(true)
	case screenContacts:
		model.activateConfigAction(false)
	}
}

// activatePresetAction edits or toggles the selected reusable preset.
func (model *Model) activatePresetAction(rate bool) {
	if model.actionCursor == 2 {
		model.actionMode = false
		return
	}
	if rate {
		preset, err := model.findRate(model.selectedRate)
		if err != nil {
			model.setError(err)
			return
		}
		if model.actionCursor == 0 {
			model.actionMode = false
			model.openRateForm(&preset)
			return
		}
		if preset.Active {
			model.actionMode = false
			model.push(screenConfirmRateToggle)
			return
		}
		if model.runCLI([]string{"rate", "restore", strconv.FormatInt(preset.ID, 10)}) {
			model.actionMode = false
			model.status = "Rate restored"
		}
		return
	}
	preset, err := model.findDescription(model.selectedDescription)
	if err != nil {
		model.setError(err)
		return
	}
	if model.actionCursor == 0 {
		model.actionMode = false
		model.openDescriptionForm(&preset)
		return
	}
	if preset.Active {
		model.actionMode = false
		model.push(screenConfirmDescriptionToggle)
		return
	}
	if model.runCLI([]string{"description", "restore", strconv.FormatInt(preset.ID, 10)}) {
		model.actionMode = false
		model.status = "Description restored"
	}
}

// activateConfigAction edits or removes a selected configuration profile.
func (model *Model) activateConfigAction(recipient bool) {
	if model.actionCursor == 2 {
		model.actionMode = false
		return
	}
	if recipient {
		if model.actionCursor == 0 {
			model.actionMode = false
			model.openRecipientForm(model.selectedConfigIndex)
			return
		}
		model.actionMode = false
		model.push(screenConfirmRecipientDelete)
		return
	}
	if model.actionCursor == 0 {
		model.actionMode = false
		model.openContactForm(model.selectedConfigIndex)
		return
	}
	model.actionMode = false
	model.push(screenConfirmContactDelete)
}

// inlineActionView renders contextual actions beneath the selected row.
func (model Model) inlineActionView() string {
	if !model.actionMode {
		return ""
	}
	lines := []string{"", mutedStyle.Render("Actions  ←/→ choose  ·  Enter activate  ·  Esc close")}
	for index, label := range model.inlineActionLabels() {
		line := "  " + label
		if index == model.actionCursor {
			line = selectedStyle.Render("› " + label)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// updateHome moves through the dashboard and opens its selected workflow.
func (model Model) updateHome(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	model.moveCursor(key, 7)
	if key.Type != tea.KeyEnter {
		return model, nil
	}
	switch model.cursor {
	case 0:
		model.openTimeForm(nil)
	case 1:
		model.openTimeEntries()
	case 2:
		model.openInvoiceRange()
	case 3:
		model.openInvoices()
	case 4:
		model.workspaceArea = workspacePresets
		model.push(screenPresets)
	case 5:
		model.openSettings()
	case 6:
		return model, tea.Quit
	}
	return model, nil
}

// loadOverview refreshes the entries shown on the landing workspace.
func (model *Model) loadOverview() {
	if model.application == nil {
		return
	}
	from := time.Date(model.now.Year(), model.now.Month(), model.now.Day(), 0, 0, 0, 0, model.now.Location())
	entries, err := model.application.Store.ListTimeEntries(context.Background(), from, from.AddDate(0, 0, 1), false)
	if err != nil {
		model.setError(err)
		return
	}
	model.overviewEntries = entries
}

// isRootScreen identifies screens that participate in workspace navigation.
func isRootScreen(active screen) bool {
	switch active {
	case screenHome, screenTimeEntries, screenInvoices, screenPresets, screenSettings:
		return true
	default:
		return false
	}
}

// handleWorkspaceNavigation switches root workspaces with horizontal arrows.
func (model *Model) handleWorkspaceNavigation(key tea.KeyMsg) bool {
	if !isRootScreen(model.screen) || (key.Type != tea.KeyLeft && key.Type != tea.KeyRight) {
		return false
	}
	if model.screen == screenTimeEntries && model.cursor < 2 {
		return false
	}
	direction := 1
	if key.Type == tea.KeyLeft {
		direction = -1
	}
	area := (model.workspaceArea + direction + 5) % 5
	model.openWorkspaceArea(area)
	return true
}

// openWorkspaceArea switches to a root workspace and refreshes its content.
func (model *Model) openWorkspaceArea(area int) {
	model.workspaceArea = area
	model.actionMode, model.actionCursor = false, 0
	model.editing, model.fields = false, nil
	model.stack = nil
	model.status, model.statusError = "", false
	switch area {
	case workspaceOverview:
		model.screen, model.cursor = screenHome, 0
		model.loadOverview()
	case workspaceTime:
		model.screen, model.cursor = screenTimeEntries, 2
		model.makeTimeEntryRangeFields()
		model.loadTimeEntries()
	case workspaceInvoices:
		model.screen, model.cursor = screenInvoices, 0
		model.loadInvoices()
	case workspacePresets:
		model.screen, model.cursor = screenPresets, 0
	case workspaceSettings:
		model.screen, model.cursor = screenSettings, 0
	}
}

// updateForm applies shared form movement, adjustment, and editing behavior.
func (model *Model) updateForm(key tea.KeyMsg, submit func()) {
	switch key.Type {
	case tea.KeyUp:
		model.moveFormCursor(-1)
	case tea.KeyDown:
		model.moveFormCursor(1)
	case tea.KeyLeft:
		model.adjustField(-1)
	case tea.KeyRight:
		model.adjustField(1)
	case tea.KeyEnter:
		if model.cursor == len(model.fields)-1 && model.fields[model.cursor].kind == fieldAction {
			submit()
			return
		}
		model.beginEdit()
	}
}

// updateEditor handles exact text entry without stealing arrows for navigation.
func (model *Model) updateEditor(key tea.KeyMsg) {
	value := []rune(model.fields[model.cursor].value)
	switch key.Type {
	case tea.KeyEsc:
		model.fields[model.cursor].value = model.editBefore
		model.editing = false
	case tea.KeyEnter:
		model.editing = false
		model.afterEdit()
	case tea.KeyLeft:
		if model.editAt > 0 {
			model.editAt--
		}
	case tea.KeyRight:
		if model.editAt < len(value) {
			model.editAt++
		}
	case tea.KeyHome:
		model.editAt = 0
	case tea.KeyEnd:
		model.editAt = len(value)
	case tea.KeyBackspace:
		if model.editAt > 0 {
			value = append(value[:model.editAt-1], value[model.editAt:]...)
			model.editAt--
			model.fields[model.cursor].value = string(value)
		}
	case tea.KeyDelete:
		if model.editAt < len(value) {
			value = append(value[:model.editAt], value[model.editAt+1:]...)
			model.fields[model.cursor].value = string(value)
		}
	case tea.KeyCtrlU:
		model.fields[model.cursor].value = ""
		model.editAt = 0
	case tea.KeyRunes, tea.KeySpace:
		inserted := key.Runes
		if key.Type == tea.KeySpace {
			inserted = []rune{' '}
		}
		if model.fields[model.cursor].kind == fieldTime {
			inserted = validTimeRunes(inserted, strings.Contains(string(value), ":"))
			if len(inserted) == 0 {
				return
			}
		}
		value = append(value, make([]rune, len(inserted))...)
		copy(value[model.editAt+len(inserted):], value[model.editAt:])
		copy(value[model.editAt:], inserted)
		model.editAt += len(inserted)
		model.fields[model.cursor].value = string(value)
		if model.fields[model.cursor].kind == fieldTime {
			model.fields[model.cursor].value, model.editAt = autoColonTime(model.fields[model.cursor].value, model.editAt)
		}
	}
}

// validTimeRunes keeps direct time entry limited to digits and one optional separator.
func validTimeRunes(input []rune, colonUsed bool) []rune {
	valid := make([]rune, 0, len(input))
	for _, character := range input {
		if character >= '0' && character <= '9' {
			valid = append(valid, character)
		} else if character == ':' && !colonUsed {
			valid = append(valid, character)
			colonUsed = true
		}
	}
	return valid
}

// autoColonTime inserts the conventional separator while still accepting a typed colon.
func autoColonTime(value string, cursor int) (string, int) {
	if strings.Contains(value, ":") {
		return value, cursor
	}
	runes := []rune(value)
	if cursor != len(runes) {
		return value, cursor
	}
	switch len(runes) {
	case 1:
		if runes[0] >= '2' && runes[0] <= '9' {
			return value + ":", cursor + 1
		}
		return value, cursor
	case 2:
		if runes[0] == '1' && runes[1] >= '3' {
			return string(runes[:1]) + ":" + string(runes[1:]), cursor + 1
		}
		return value + ":", cursor + 1
	case 3:
		return string(runes[:1]) + ":" + string(runes[1:]), cursor + 1
	case 4:
		return string(runes[:2]) + ":" + string(runes[2:]), cursor + 1
	default:
		return value, cursor
	}
}

// beginEdit switches editable fields into exact input mode.
func (model *Model) beginEdit() {
	item := &model.fields[model.cursor]
	if item.kind == fieldAction || (item.kind == fieldChoice && item.choiceID() != 0) {
		return
	}
	model.editing = true
	model.editBefore = item.value
	model.editAt = len([]rune(item.value))
}

// afterEdit reloads lists whose date filters were edited.
func (model *Model) afterEdit() {
	if model.screen == screenTimeForm && model.fields[model.cursor].kind == fieldTime {
		if normalized, err := normalizeClock(model.fields[model.cursor].value); err == nil {
			model.fields[model.cursor].value = normalized
		} else {
			model.setError(err)
		}
	}
	if model.screen == screenTimeEntries {
		model.loadTimeEntries()
	}
}

// adjustField changes date/time/choice values with one arrow press.
func (model *Model) adjustField(direction int) {
	if model.cursor < 0 || model.cursor >= len(model.fields) {
		return
	}
	item := &model.fields[model.cursor]
	switch item.kind {
	case fieldDate:
		if parsed, err := time.ParseInLocation("2006-01-02", item.value, time.Local); err == nil {
			item.value = parsed.AddDate(0, 0, direction).Format("2006-01-02")
		}
	case fieldTime:
		model.adjustTimeField(model.cursor, direction)
	case fieldChoice:
		if len(item.choices) == 0 {
			return
		}
		item.choiceAt = (item.choiceAt + direction + len(item.choices)) % len(item.choices)
		item.value = item.choices[item.choiceAt].value
	}
	if model.screen == screenTimeEntries && (model.cursor == 0 || model.cursor == 1) {
		model.loadTimeEntries()
	}
}

// moveFormCursor uses vertical arrows so horizontal arrows remain available to adjust values.
func (model *Model) moveFormCursor(direction int) {
	if len(model.fields) == 0 {
		model.cursor = 0
		return
	}
	model.cursor = (model.cursor + direction + len(model.fields)) % len(model.fields)
}

// adjustTimeField changes a 12-hour value by fifteen minutes and keeps its period in sync.
func (model *Model) adjustTimeField(index, direction int) {
	periodIndex := timeStartPeriodField
	if index == timeEndField {
		periodIndex = timeEndPeriodField
	}
	period := model.fields[periodIndex].value
	parsed, err := time.Parse("03:04 PM", model.fields[index].value+" "+period)
	if err != nil {
		minute := model.now.Minute() / 15 * 15
		parsed = time.Date(0, 1, 1, model.now.Hour(), minute, 0, 0, time.UTC)
	}
	parsed = parsed.Add(time.Duration(direction) * 15 * time.Minute)
	model.fields[index].value = parsed.Format("03:04")
	model.fields[periodIndex].value = parsed.Format("PM")
	model.fields[periodIndex].choiceAt = 0
	if model.fields[periodIndex].value == "PM" {
		model.fields[periodIndex].choiceAt = 1
	}
}

// moveCursor wraps Up and Down within the active item count.
func (model *Model) moveCursor(key tea.KeyMsg, count int) {
	if count <= 0 {
		model.cursor = 0
		return
	}
	switch key.Type {
	case tea.KeyUp:
		model.cursor = (model.cursor - 1 + count) % count
	case tea.KeyDown:
		model.cursor = (model.cursor + 1) % count
	}
}

// push opens a screen while remembering the current selection.
func (model *Model) push(next screen) {
	model.stack = append(model.stack, location{screen: model.screen, cursor: model.cursor, fields: cloneFields(model.fields)})
	model.screen, model.cursor, model.fields = next, 0, nil
	model.actionMode, model.actionCursor = false, 0
	model.status, model.statusError = "", false
}

// back returns to the previous screen and selection.
func (model *Model) back() {
	if model.screen == screenTimeConfirm {
		model.screen, model.cursor = screenTimeForm, timeSaveField
		model.status, model.statusError = "", false
		return
	}
	if model.actionMode {
		model.actionMode, model.actionCursor = false, 0
		return
	}
	if len(model.stack) == 0 {
		if model.screen != screenHome {
			model.openWorkspaceArea(workspaceOverview)
		}
		return
	}
	last := model.stack[len(model.stack)-1]
	model.stack = model.stack[:len(model.stack)-1]
	model.screen, model.cursor = last.screen, last.cursor
	model.fields = cloneFields(last.fields)
	model.editing = false
	switch model.screen {
	case screenHome:
		model.workspaceArea = workspaceOverview
	case screenTimeEntries:
		model.workspaceArea = workspaceTime
		model.loadTimeEntries()
	case screenInvoices:
		model.workspaceArea = workspaceInvoices
		model.loadInvoices()
	case screenPresets, screenRates, screenDescriptions:
		model.workspaceArea = workspacePresets
	case screenSettings, screenSettingsForm, screenRecipients, screenContacts:
		model.workspaceArea = workspaceSettings
	}
}

// cloneFields keeps form values local to their screen in the back stack.
func cloneFields(fields []field) []field {
	cloned := append([]field(nil), fields...)
	for index := range cloned {
		cloned[index].choices = append([]choice(nil), cloned[index].choices...)
	}
	return cloned
}

// openTimeForm loads defaults or an existing entry into the shared time form.
func (model *Model) openTimeForm(entry *domain.TimeEntry) {
	model.workspaceArea = workspaceTime
	model.push(screenTimeForm)
	descriptionChoices, rateChoices := model.loadPresetChoices()
	date, start, startPeriod := model.now.Format("2006-01-02"), "", "AM"
	endDate, end, endPeriod, notes := model.now.Format("2006-01-02"), "", "AM", ""
	model.timeCurrency = model.application.Config.Currency
	if entry != nil {
		model.timeEntryID = entry.ID
		date, start, startPeriod = entry.StartAt.Local().Format("2006-01-02"), entry.StartAt.Local().Format("03:04"), entry.StartAt.Local().Format("PM")
		endDate, end, endPeriod, notes = entry.EndAt.Local().Format("2006-01-02"), entry.EndAt.Local().Format("03:04"), entry.EndAt.Local().Format("PM"), entry.Notes
		model.timeCurrency = entry.Currency
	} else {
		model.timeEntryID = 0
	}
	description := choiceField("Description", descriptionChoices, "Type a description")
	rate := choiceField("Rate", rateChoices, "0.00")
	if entry != nil {
		selectChoice(&description, entry.DescriptionPresetID, entry.Description)
		selectChoice(&rate, entry.RatePresetID, domain.FormatMoney(entry.RateAmountCents))
	}
	model.fields = []field{
		{label: "Start Date", value: date, kind: fieldDate},
		{label: "Start Time", value: start, placeholder: "Enter H:MM", kind: fieldTime},
		periodField("Start AM/PM", startPeriod),
		{label: "End Date", value: endDate, kind: fieldDate},
		{label: "End Time", value: end, placeholder: "Enter H:MM", kind: fieldTime},
		periodField("End AM/PM", endPeriod),
		description,
		rate,
		{label: "Notes", value: notes, placeholder: "Optional", kind: fieldText},
		{label: "Review Entry", kind: fieldAction},
	}
}

// periodField creates an arrow-selectable AM/PM field.
func periodField(label, value string) field {
	choiceAt := 0
	if value == "PM" {
		choiceAt = 1
	}
	return field{label: label, value: value, kind: fieldChoice, choiceAt: choiceAt, choices: []choice{{label: "AM", value: "AM", id: 1}, {label: "PM", value: "PM", id: 2}}}
}

// loadPresetChoices converts active presets into arrow-selectable values with a Custom fallback.
func (model *Model) loadPresetChoices() ([]choice, []choice) {
	ctx := context.Background()
	descriptions, descriptionErr := model.application.Store.ListDescriptionPresets(ctx, false)
	rates, rateErr := model.application.Store.ListRatePresets(ctx, false)
	if descriptionErr != nil {
		model.setError(descriptionErr)
	}
	if rateErr != nil {
		model.setError(rateErr)
	}
	descriptionChoices := make([]choice, 0, len(descriptions)+1)
	for _, preset := range descriptions {
		descriptionChoices = append(descriptionChoices, choice{label: preset.Label, value: preset.Label, id: preset.ID})
	}
	descriptionChoices = append(descriptionChoices, choice{label: "Custom", id: 0})
	rateChoices := make([]choice, 0, len(rates)+1)
	for _, preset := range rates {
		rateChoices = append(rateChoices, choice{
			label: fmt.Sprintf("%s · %s %s", preset.Label, domain.FormatMoney(preset.AmountCents), preset.Currency),
			value: domain.FormatMoney(preset.AmountCents), id: preset.ID,
		})
	}
	rateChoices = append(rateChoices, choice{label: "Custom", id: 0})
	return descriptionChoices, rateChoices
}

// choiceField creates a field defaulting to its first active preset.
func choiceField(label string, choices []choice, placeholder string) field {
	item := field{label: label, kind: fieldChoice, choices: choices, placeholder: placeholder}
	if len(choices) > 0 {
		item.value = choices[0].value
	}
	return item
}

// selectChoice restores a linked preset or falls back to the Custom option.
func selectChoice(item *field, selectedID *int64, customValue string) {
	if selectedID != nil {
		for index, option := range item.choices {
			if option.id == *selectedID {
				item.choiceAt, item.value = index, option.value
				return
			}
		}
	}
	item.choiceAt = len(item.choices) - 1
	item.value = customValue
}

// submitTimeForm validates the interval and opens the final confirmation screen.
func (model *Model) submitTimeForm() {
	start, end, err := model.timeFormInterval()
	if err != nil {
		model.setError(err)
		return
	}
	model.timeStartAt, model.timeEndAt = start, end
	model.screen, model.cursor = screenTimeConfirm, 0
	model.status, model.statusError = "", false
}

// updateTimeConfirmation saves the reviewed entry or returns to its editable fields.
func (model *Model) updateTimeConfirmation(key tea.KeyMsg) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 1 {
		model.back()
		return
	}
	model.saveTimeEntry()
}

// saveTimeEntry persists a confirmed interval through the shared CLI behavior.
func (model *Model) saveTimeEntry() {
	args := []string{"time", "add"}
	if model.timeEntryID != 0 {
		args = []string{"time", "edit", strconv.FormatInt(model.timeEntryID, 10)}
	}
	args = append(args,
		"--start", model.timeStartAt.Format("2006-01-02 15:04"),
		"--end", model.timeEndAt.Format("2006-01-02 15:04"),
	)
	if id := model.fields[timeDescriptionField].choiceID(); id != 0 {
		args = append(args, "--description-preset", strconv.FormatInt(id, 10))
	} else {
		args = append(args, "--description", model.fields[timeDescriptionField].value)
	}
	if id := model.fields[timeRateField].choiceID(); id != 0 {
		args = append(args, "--rate-preset", strconv.FormatInt(id, 10))
	} else {
		args = append(args, "--rate", model.fields[timeRateField].value, "--currency", model.timeCurrency)
	}
	args = append(args, "--notes", model.fields[timeNotesField].value)
	if !model.runCLI(args) {
		return
	}
	message := model.status
	model.workspaceArea = workspaceTime
	model.screen = screenTimeEntries
	model.stack = []location{{screen: screenHome, cursor: 1}}
	model.cursor = 3
	model.makeTimeEntryRangeFields()
	model.loadTimeEntries()
	model.status = message
}

// timeFormInterval parses the form's 12-hour inputs and rejects non-positive durations.
func (model Model) timeFormInterval() (time.Time, time.Time, error) {
	start, err := parseFormDateTime(model.fields[timeDateField].value, model.fields[timeStartField].value, model.fields[timeStartPeriodField].value)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start time: %w", err)
	}
	end, err := parseFormDateTime(model.fields[timeEndDateField].value, model.fields[timeEndField].value, model.fields[timeEndPeriodField].value)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end time: %w", err)
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errors.New("end time must be after start time")
	}
	return start, end, nil
}

// parseFormDateTime combines one local date with a normalized 12-hour clock value.
func parseFormDateTime(dateText, clockText, period string) (time.Time, error) {
	clock, err := normalizeClock(clockText)
	if err != nil {
		return time.Time{}, err
	}
	if period != "AM" && period != "PM" {
		return time.Time{}, errors.New("choose AM or PM")
	}
	parsed, err := time.ParseInLocation("2006-01-02 03:04 PM", dateText+" "+clock+" "+period, time.Local)
	if err != nil {
		return time.Time{}, errors.New("use YYYY-MM-DD and a valid time")
	}
	return parsed, nil
}

// normalizeClock accepts typed HMM, HHMM, H:MM, or HH:MM values.
func normalizeClock(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, ":") {
		switch len(value) {
		case 3:
			value = value[:1] + ":" + value[1:]
		case 4:
			value = value[:2] + ":" + value[2:]
		}
	}
	parsed, err := time.Parse("3:04", value)
	if err != nil || parsed.Hour() < 1 || parsed.Hour() > 12 {
		return "", errors.New("use a 12-hour time such as 9:30")
	}
	return parsed.Format("03:04"), nil
}

// formatInterval renders an exact minute-based duration for display.
func formatInterval(duration time.Duration) string {
	hours := int(duration / time.Hour)
	minutes := int(duration%time.Hour) / int(time.Minute)
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// openTimeEntries shows today's entries and range controls.
func (model *Model) openTimeEntries() {
	model.workspaceArea = workspaceTime
	model.push(screenTimeEntries)
	model.makeTimeEntryRangeFields()
	model.loadTimeEntries()
}

// makeTimeEntryRangeFields defaults both endpoints to today.
func (model *Model) makeTimeEntryRangeFields() {
	today := model.now.Format("2006-01-02")
	model.fields = []field{{label: "From", value: today, kind: fieldDate}, {label: "To", value: today, kind: fieldDate}}
}

// loadTimeEntries refreshes rows for the inclusive range controls.
func (model *Model) loadTimeEntries() {
	if len(model.fields) < 2 {
		return
	}
	from, err := time.ParseInLocation("2006-01-02", model.fields[0].value, time.Local)
	if err != nil {
		model.setError(fmt.Errorf("from date: %w", err))
		return
	}
	to, err := time.ParseInLocation("2006-01-02", model.fields[1].value, time.Local)
	if err != nil || to.Before(from) {
		model.setError(fmt.Errorf("to date must not be before from date"))
		return
	}
	model.entries, err = model.application.Store.ListTimeEntries(context.Background(), from, to.AddDate(0, 0, 1), false)
	if err != nil {
		model.setError(err)
	}
}

// updateTimeEntries navigates range controls, Add, and stored rows.
func (model *Model) updateTimeEntries(key tea.KeyMsg) {
	model.moveCursor(key, len(model.entries)+3)
	if model.cursor < 2 && (key.Type == tea.KeyLeft || key.Type == tea.KeyRight) {
		direction := -1
		if key.Type == tea.KeyRight {
			direction = 1
		}
		model.adjustField(direction)
		return
	}
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor < 2 {
		model.beginEdit()
		return
	}
	if model.cursor == 2 {
		model.openTimeForm(nil)
		return
	}
	model.selectedEntryID = model.entries[model.cursor-3].ID
	model.actionMode, model.actionCursor = true, 0
}

// updateTimeEntryActions opens edit/delete actions for one row.
func (model *Model) updateTimeEntryActions(key tea.KeyMsg) {
	model.moveCursor(key, 3)
	if key.Type != tea.KeyEnter {
		return
	}
	switch model.cursor {
	case 0:
		entry, err := model.application.Store.GetTimeEntry(context.Background(), model.selectedEntryID)
		if err != nil {
			model.setError(err)
			return
		}
		model.openTimeForm(&entry)
	case 1:
		model.push(screenConfirmTimeDelete)
	case 2:
		model.back()
	}
}

// updateTimeDeleteConfirmation requires an explicit destructive choice.
func (model *Model) updateTimeDeleteConfirmation(key tea.KeyMsg) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.back()
		return
	}
	if model.runCLI([]string{"time", "delete", strconv.FormatInt(model.selectedEntryID, 10)}) {
		message := "Time entry deleted"
		model.screen = screenTimeEntries
		if len(model.stack) >= 2 {
			model.stack = model.stack[:len(model.stack)-2]
		}
		model.cursor = 2
		model.makeTimeEntryRangeFields()
		model.loadTimeEntries()
		model.status = message
	}
}

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
	recipients := make([]choice, 0, len(model.application.Config.Recipients))
	for index, recipient := range model.application.Config.Recipients {
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
		{label: "Terms", value: model.application.Config.PayableTerms, kind: fieldText},
		{label: "Notes", value: model.application.Config.Notes, kind: fieldText},
		{label: "Adjustment", value: model.application.Config.DefaultAdjustment, kind: fieldText},
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
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 1 {
		model.back()
		return
	}
	model.runCLI([]string{"invoice", "export", strconv.FormatInt(model.selectedInvoice, 10)})
}

// updateSimpleMenu applies shared movement to short submenus.
func (model *Model) updateSimpleMenu(key tea.KeyMsg, count int, open func()) {
	model.moveCursor(key, count)
	if key.Type == tea.KeyEnter {
		open()
	}
}

// openPresetSelection opens existing rate/description data without another command prompt.
func (model *Model) openPresetSelection() {
	switch model.cursor {
	case 0:
		model.workspaceArea = workspacePresets
		model.push(screenRates)
	case 1:
		model.workspaceArea = workspacePresets
		model.push(screenDescriptions)
	case 2:
		model.back()
	}
}

// updateRates navigates Add and every active or inactive rate.
func (model *Model) updateRates(key tea.KeyMsg) {
	presets, err := model.application.Store.ListRatePresets(context.Background(), true)
	if err != nil {
		model.setError(err)
		return
	}
	model.moveCursor(key, len(presets)+1)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.openRateForm(nil)
		return
	}
	model.selectedRate = presets[model.cursor-1].ID
	model.actionMode, model.actionCursor = true, 0
}

// openRateForm creates fields for adding or editing a reusable price.
func (model *Model) openRateForm(preset *domain.RatePreset) {
	model.workspaceArea = workspacePresets
	model.push(screenRateForm)
	label, amount, currency := "", "", model.application.Config.Currency
	model.selectedRate = 0
	if preset != nil {
		model.selectedRate = preset.ID
		label, amount, currency = preset.Label, domain.FormatMoney(preset.AmountCents), preset.Currency
	}
	model.fields = []field{
		{label: "Label", value: label, kind: fieldText},
		{label: "Amount", value: amount, placeholder: "0.00", kind: fieldText},
		{label: "Currency", value: currency, kind: fieldText},
		{label: "Save Rate", kind: fieldAction},
	}
}

// saveRate persists a new or edited preset through the shared CLI.
func (model *Model) saveRate() {
	args := []string{"rate", "add", model.fields[rateLabelField].value, model.fields[rateAmountField].value, model.fields[rateCurrencyField].value}
	if model.selectedRate != 0 {
		args = []string{"rate", "update", strconv.FormatInt(model.selectedRate, 10), model.fields[rateLabelField].value, model.fields[rateAmountField].value, model.fields[rateCurrencyField].value}
	}
	if model.runCLI(args) {
		message := model.status
		model.back()
		model.status = message
	}
}

// updateRateActions edits, disables, or restores the selected rate.
func (model *Model) updateRateActions(key tea.KeyMsg) {
	model.moveCursor(key, 3)
	if key.Type != tea.KeyEnter {
		return
	}
	preset, err := model.findRate(model.selectedRate)
	if err != nil {
		model.setError(err)
		return
	}
	switch model.cursor {
	case 0:
		model.openRateForm(&preset)
	case 1:
		if preset.Active {
			model.push(screenConfirmRateToggle)
		} else if model.runCLI([]string{"rate", "restore", strconv.FormatInt(preset.ID, 10)}) {
			message := "Rate restored"
			model.back()
			model.status = message
		}
	case 2:
		model.back()
	}
}

// updateRateToggleConfirmation requires confirmation before disabling a preset.
func (model *Model) updateRateToggleConfirmation(key tea.KeyMsg) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.back()
		return
	}
	if model.runCLI([]string{"rate", "delete", strconv.FormatInt(model.selectedRate, 10)}) {
		model.leaveTwoScreens(screenRates, "Rate disabled")
	}
}

// findRate locates a rate preset for editing and state changes.
func (model *Model) findRate(id int64) (domain.RatePreset, error) {
	presets, err := model.application.Store.ListRatePresets(context.Background(), true)
	if err != nil {
		return domain.RatePreset{}, err
	}
	for _, preset := range presets {
		if preset.ID == id {
			return preset, nil
		}
	}
	return domain.RatePreset{}, fmt.Errorf("rate preset %d not found", id)
}

// updateDescriptions navigates Add and every active or inactive description.
func (model *Model) updateDescriptions(key tea.KeyMsg) {
	presets, err := model.application.Store.ListDescriptionPresets(context.Background(), true)
	if err != nil {
		model.setError(err)
		return
	}
	model.moveCursor(key, len(presets)+1)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.openDescriptionForm(nil)
		return
	}
	model.selectedDescription = presets[model.cursor-1].ID
	model.actionMode, model.actionCursor = true, 0
}

// openDescriptionForm creates fields for adding or editing reusable text.
func (model *Model) openDescriptionForm(preset *domain.DescriptionPreset) {
	model.workspaceArea = workspacePresets
	model.push(screenDescriptionForm)
	label := ""
	model.selectedDescription = 0
	if preset != nil {
		model.selectedDescription, label = preset.ID, preset.Label
	}
	model.fields = []field{{label: "Description", value: label, kind: fieldText}, {label: "Save Description", kind: fieldAction}}
}

// saveDescription persists a new or edited description through the shared CLI.
func (model *Model) saveDescription() {
	args := []string{"description", "add", model.fields[descriptionLabelField].value}
	if model.selectedDescription != 0 {
		args = []string{"description", "update", strconv.FormatInt(model.selectedDescription, 10), model.fields[descriptionLabelField].value}
	}
	if model.runCLI(args) {
		message := model.status
		model.back()
		model.status = message
	}
}

// updateDescriptionActions edits, disables, or restores the selected description.
func (model *Model) updateDescriptionActions(key tea.KeyMsg) {
	model.moveCursor(key, 3)
	if key.Type != tea.KeyEnter {
		return
	}
	preset, err := model.findDescription(model.selectedDescription)
	if err != nil {
		model.setError(err)
		return
	}
	switch model.cursor {
	case 0:
		model.openDescriptionForm(&preset)
	case 1:
		if preset.Active {
			model.push(screenConfirmDescriptionToggle)
		} else if model.runCLI([]string{"description", "restore", strconv.FormatInt(preset.ID, 10)}) {
			message := "Description restored"
			model.back()
			model.status = message
		}
	case 2:
		model.back()
	}
}

// updateDescriptionToggleConfirmation requires confirmation before disabling a preset.
func (model *Model) updateDescriptionToggleConfirmation(key tea.KeyMsg) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.back()
		return
	}
	if model.runCLI([]string{"description", "delete", strconv.FormatInt(model.selectedDescription, 10)}) {
		model.leaveTwoScreens(screenDescriptions, "Description disabled")
	}
}

// findDescription locates a description preset for editing and state changes.
func (model *Model) findDescription(id int64) (domain.DescriptionPreset, error) {
	presets, err := model.application.Store.ListDescriptionPresets(context.Background(), true)
	if err != nil {
		return domain.DescriptionPreset{}, err
	}
	for _, preset := range presets {
		if preset.ID == id {
			return preset, nil
		}
	}
	return domain.DescriptionPreset{}, fmt.Errorf("description preset %d not found", id)
}

// leaveTwoScreens returns from an action and confirmation pair to its list.
func (model *Model) leaveTwoScreens(target screen, message string) {
	if len(model.stack) >= 2 {
		model.stack = model.stack[:len(model.stack)-2]
	}
	model.screen, model.cursor, model.fields = target, 0, nil
	model.status, model.statusError = message, false
}

// openSettings opens the four configuration groups.
func (model *Model) openSettings() {
	model.workspaceArea = workspaceSettings
	model.push(screenSettings)
}

// updateSettings opens sender, recipient, contact, or invoice-default management.
func (model *Model) updateSettings(key tea.KeyMsg) {
	model.moveCursor(key, 5)
	if key.Type != tea.KeyEnter {
		return
	}
	switch model.cursor {
	case 0:
		model.openSettingsForm(false)
	case 1:
		model.push(screenRecipients)
	case 2:
		model.push(screenContacts)
	case 3:
		model.openSettingsForm(true)
	case 4:
		model.back()
	}
}

// openSettingsForm loads either sender or invoice-default values.
func (model *Model) openSettingsForm(defaults bool) {
	model.workspaceArea = workspaceSettings
	model.push(screenSettingsForm)
	model.settingsDefaults = defaults
	cfg := model.application.Config
	if defaults {
		model.fields = []field{
			{label: "Terms", value: cfg.PayableTerms, kind: fieldText},
			{label: "Currency", value: cfg.Currency, kind: fieldText},
			{label: "Logo", value: cfg.LogoPath, placeholder: "Optional", kind: fieldText},
			{label: "Output Directory", value: cfg.OutputDir, kind: fieldText},
			{label: "Default Notes", value: cfg.Notes, kind: fieldText},
			{label: "Default Adjustment", value: cfg.DefaultAdjustment, kind: fieldText},
			{label: "Save Defaults", kind: fieldAction},
		}
		return
	}
	model.fields = []field{
		{label: "Name", value: cfg.Sender.FullName, kind: fieldText},
		{label: "Address", value: cfg.Sender.Address, kind: fieldText},
		{label: "Email", value: cfg.Sender.Email, kind: fieldText},
		{label: "Save Sender", kind: fieldAction},
	}
}

// saveSettingsForm validates and atomically saves one configuration group.
func (model *Model) saveSettingsForm() {
	cfg := model.application.Config
	if model.settingsDefaults {
		cfg.PayableTerms, cfg.Currency, cfg.LogoPath = model.fields[0].value, model.fields[1].value, model.fields[2].value
		cfg.OutputDir, cfg.Notes, cfg.DefaultAdjustment = model.fields[3].value, model.fields[4].value, model.fields[5].value
	} else {
		cfg.Sender.FullName, cfg.Sender.Address, cfg.Sender.Email = model.fields[senderNameField].value, model.fields[senderAddressField].value, model.fields[senderEmailField].value
	}
	if !model.saveConfig(cfg, "Settings saved") {
		return
	}
	message := model.status
	model.back()
	model.status = message
}

// updateRecipients navigates Add and recipient profiles.
func (model *Model) updateRecipients(key tea.KeyMsg) {
	model.moveCursor(key, len(model.application.Config.Recipients)+1)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.openRecipientForm(-1)
		return
	}
	model.selectedConfigIndex = model.cursor - 1
	model.actionMode, model.actionCursor = true, 0
}

// openRecipientForm creates fields for a new or existing recipient.
func (model *Model) openRecipientForm(index int) {
	model.workspaceArea = workspaceSettings
	model.push(screenRecipientForm)
	model.selectedConfigIndex = index
	recipient := config.Recipient{}
	if index >= 0 && index < len(model.application.Config.Recipients) {
		recipient = model.application.Config.Recipients[index]
	}
	model.fields = []field{
		{label: "Company", value: recipient.CompanyName, kind: fieldText},
		{label: "Address", value: recipient.Address, kind: fieldText},
		{label: "Save Recipient", kind: fieldAction},
	}
}

// saveRecipient adds or updates a recipient profile.
func (model *Model) saveRecipient() {
	cfg := model.application.Config
	recipient := config.Recipient{CompanyName: model.fields[0].value, Address: model.fields[1].value}
	if model.selectedConfigIndex < 0 {
		cfg.Recipients = append(cfg.Recipients, recipient)
	} else {
		cfg.Recipients[model.selectedConfigIndex] = recipient
	}
	if model.saveConfig(cfg, "Recipient saved") {
		message := model.status
		model.back()
		model.status = message
	}
}

// updateRecipientActions edits or requests deletion of one recipient.
func (model *Model) updateRecipientActions(key tea.KeyMsg) {
	model.moveCursor(key, 3)
	if key.Type != tea.KeyEnter {
		return
	}
	switch model.cursor {
	case 0:
		model.openRecipientForm(model.selectedConfigIndex)
	case 1:
		model.push(screenConfirmRecipientDelete)
	case 2:
		model.back()
	}
}

// updateRecipientDeleteConfirmation protects recipient removal.
func (model *Model) updateRecipientDeleteConfirmation(key tea.KeyMsg) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.back()
		return
	}
	cfg := model.application.Config
	if len(cfg.Recipients) == 1 {
		model.setError(fmt.Errorf("at least one recipient profile is required"))
		return
	}
	index := model.selectedConfigIndex
	cfg.Recipients = append(cfg.Recipients[:index], cfg.Recipients[index+1:]...)
	if model.saveConfig(cfg, "Recipient deleted") {
		model.leaveTwoScreens(screenRecipients, "Recipient deleted")
	}
}

// updateContacts navigates Add and contact profiles.
func (model *Model) updateContacts(key tea.KeyMsg) {
	model.moveCursor(key, len(model.application.Config.Contacts)+1)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.openContactForm(-1)
		return
	}
	model.selectedConfigIndex = model.cursor - 1
	model.actionMode, model.actionCursor = true, 0
}

// openContactForm creates fields for a new or existing contact.
func (model *Model) openContactForm(index int) {
	model.workspaceArea = workspaceSettings
	model.push(screenContactForm)
	model.selectedConfigIndex = index
	contact := config.Contact{}
	if index >= 0 && index < len(model.application.Config.Contacts) {
		contact = model.application.Config.Contacts[index]
	}
	model.fields = []field{
		{label: "Name", value: contact.Name, kind: fieldText},
		{label: "Email", value: contact.Email, kind: fieldText},
		{label: "Save Contact", kind: fieldAction},
	}
}

// saveContact adds or updates a contact profile.
func (model *Model) saveContact() {
	cfg := model.application.Config
	contact := config.Contact{Name: model.fields[0].value, Email: model.fields[1].value}
	if model.selectedConfigIndex < 0 {
		cfg.Contacts = append(cfg.Contacts, contact)
	} else {
		cfg.Contacts[model.selectedConfigIndex] = contact
	}
	if model.saveConfig(cfg, "Contact saved") {
		message := model.status
		model.back()
		model.status = message
	}
}

// updateContactActions edits or requests deletion of one contact.
func (model *Model) updateContactActions(key tea.KeyMsg) {
	model.moveCursor(key, 3)
	if key.Type != tea.KeyEnter {
		return
	}
	switch model.cursor {
	case 0:
		model.openContactForm(model.selectedConfigIndex)
	case 1:
		model.push(screenConfirmContactDelete)
	case 2:
		model.back()
	}
}

// updateContactDeleteConfirmation protects contact removal.
func (model *Model) updateContactDeleteConfirmation(key tea.KeyMsg) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.back()
		return
	}
	cfg := model.application.Config
	index := model.selectedConfigIndex
	cfg.Contacts = append(cfg.Contacts[:index], cfg.Contacts[index+1:]...)
	if model.saveConfig(cfg, "Contact deleted") {
		model.leaveTwoScreens(screenContacts, "Contact deleted")
	}
}

// saveConfig persists one validated configuration snapshot.
func (model *Model) saveConfig(cfg config.Config, message string) bool {
	if err := config.Save(model.application.Paths.ConfigFile, cfg); err != nil {
		model.setError(err)
		return false
	}
	model.application.Config = cfg
	model.status, model.statusError = message, false
	return true
}

// runCLI captures shared command feedback without leaving the active screen on failure.
func (model *Model) runCLI(args []string) bool {
	var output bytes.Buffer
	model.partialInvoice = 0
	err := cli.Run(context.Background(), model.application, args, &output, &output)
	message := strings.TrimSpace(output.String())
	if err != nil {
		var partial *cli.FinalizedInvoiceError
		if errors.As(err, &partial) {
			model.partialInvoice = partial.InvoiceID
		}
		if message != "" {
			message += ": "
		}
		model.status, model.statusError = message+err.Error(), true
		return false
	}
	if message == "" {
		message = "Done"
	}
	model.status, model.statusError = message, false
	return true
}

// setError displays an operational error without terminating the program.
func (model *Model) setError(err error) {
	model.status, model.statusError = err.Error(), true
}

// View renders the active screen in a responsive bordered layout.
func (model Model) View() string {
	if model.width > 0 && (model.width < 72 || model.height < 21) {
		return "EZ INVOICE\n\nThis terminal is too small. Resize to at least 72 × 21.\n\nCtrl+C quits."
	}
	width := model.width
	if width == 0 {
		width = 84
	}
	panelWidth := width - 6
	if panelWidth > 92 {
		panelWidth = 92
	}
	header := accentStyle.Render("EZ INVOICE") + mutedStyle.Render("  ·  local time & billing  ") + model.workspaceNavView()
	breadcrumb := mutedStyle.Render(model.breadcrumb())
	body := panelStyle.Width(panelWidth).Render(model.screenView())
	footer := mutedStyle.Render(model.helpText())
	status := ""
	if model.status != "" {
		style := successStyle
		if model.statusError {
			style = errorStyle
		}
		status = "\n" + style.Render(model.status)
	}
	return strings.Join([]string{header, breadcrumb, "", body, status, "", footer}, "\n")
}

// workspaceNavView renders the active root area without adding vertical layout cost.
func (model Model) workspaceNavView() string {
	labels := []string{"Overview", "Time", "Invoices", "Presets", "Settings"}
	lines := make([]string, len(labels))
	for index, label := range labels {
		if index == model.workspaceArea {
			lines[index] = selectedStyle.Render(label)
		} else {
			lines[index] = mutedStyle.Render(label)
		}
	}
	return strings.Join(lines, mutedStyle.Render(" · "))
}

// breadcrumb identifies the active workflow.
func (model Model) breadcrumb() string {
	names := map[screen]string{
		screenHome: "Overview", screenTimeForm: "Overview / Time Entry", screenTimeConfirm: "Overview / Time Entry / Confirm", screenTimeEntries: "Time",
		screenTimeEntryActions: "Home / Time Entries / Entry", screenConfirmTimeDelete: "Home / Time Entries / Delete",
		screenInvoiceRange: "Invoices / Build / Dates", screenInvoiceEntries: "Invoices / Build / Entries",
		screenInvoiceMetadata: "Invoices / Build / Details", screenInvoiceReview: "Invoices / Build / Review",
		screenInvoices: "Invoices", screenInvoiceActions: "Invoices / Invoice",
		screenPresets: "Presets", screenRates: "Presets / Rates", screenRateForm: "Presets / Rates / Edit",
		screenRateActions: "Home / Presets / Rates / Actions", screenConfirmRateToggle: "Home / Presets / Rates / Disable",
		screenDescriptions: "Home / Presets / Descriptions", screenDescriptionForm: "Home / Presets / Descriptions / Edit",
		screenDescriptionActions: "Home / Presets / Descriptions / Actions", screenConfirmDescriptionToggle: "Home / Presets / Descriptions / Disable",
		screenSettings: "Settings", screenSettingsForm: "Settings / Edit", screenRecipients: "Settings / Recipients",
		screenRecipientForm: "Settings / Recipients / Edit", screenRecipientActions: "Settings / Recipients / Actions",
		screenConfirmRecipientDelete: "Settings / Recipients / Delete", screenContacts: "Settings / Contacts",
		screenContactForm: "Settings / Contacts / Edit", screenContactActions: "Settings / Contacts / Actions",
		screenConfirmContactDelete: "Settings / Contacts / Delete",
	}
	return names[model.screen]
}

// screenView renders only the current panel contents.
func (model Model) screenView() string {
	switch model.screen {
	case screenHome:
		return model.homeView()
	case screenTimeForm, screenInvoiceRange, screenInvoiceMetadata, screenRateForm, screenDescriptionForm, screenSettingsForm, screenRecipientForm, screenContactForm:
		return model.formView()
	case screenTimeConfirm:
		return model.timeConfirmationView()
	case screenTimeEntries:
		return model.timeEntriesView()
	case screenTimeEntryActions:
		return model.menuView("Time Entry", []string{"Edit", "Delete", "Back"}, []string{"Change this uninvoiced entry", "Remove it after confirmation", "Return to the list"})
	case screenConfirmTimeDelete:
		return model.menuView("Delete this time entry?", []string{"Cancel", "Delete"}, []string{"Keep the entry", "Permanently remove this uninvoiced entry"})
	case screenInvoiceEntries:
		return model.invoiceEntriesView()
	case screenInvoiceReview:
		return model.invoiceReviewView()
	case screenInvoices:
		return model.invoicesView()
	case screenInvoiceActions:
		return model.invoiceActionsView()
	case screenPresets:
		return model.menuView("Presets", []string{"Rates", "Descriptions", "Back"}, []string{"Browse reusable prices", "Browse reusable work descriptions", "Return home"})
	case screenRates:
		return model.ratesView()
	case screenRateActions:
		return model.presetActionsView(true)
	case screenConfirmRateToggle:
		return model.menuView("Disable this rate?", []string{"Cancel", "Disable"}, []string{"Keep the rate active", "Hide it from new time entries"})
	case screenDescriptions:
		return model.descriptionsView()
	case screenDescriptionActions:
		return model.presetActionsView(false)
	case screenConfirmDescriptionToggle:
		return model.menuView("Disable this description?", []string{"Cancel", "Disable"}, []string{"Keep the description active", "Hide it from new time entries"})
	case screenSettings:
		return model.menuView("Settings", []string{"Sender", "Recipients", "Contacts", "Invoice Defaults", "Back"},
			[]string{"Name, address, and email", "Companies you invoice", "Recipient points of contact", "Terms, currency, output, and notes", "Return home"})
	case screenRecipients:
		return model.recipientsView()
	case screenRecipientActions:
		return model.menuView("Recipient", []string{"Edit", "Delete", "Back"}, []string{"Change this recipient", "Remove it after confirmation", "Return to recipients"})
	case screenConfirmRecipientDelete:
		return model.menuView("Delete this recipient?", []string{"Cancel", "Delete"}, []string{"Keep this recipient", "Permanently remove this profile"})
	case screenContacts:
		return model.contactsView()
	case screenContactActions:
		return model.menuView("Contact", []string{"Edit", "Delete", "Back"}, []string{"Change this contact", "Remove it after confirmation", "Return to contacts"})
	case screenConfirmContactDelete:
		return model.menuView("Delete this contact?", []string{"Cancel", "Delete"}, []string{"Keep this contact", "Permanently remove this profile"})
	}
	return ""
}

// homeView renders the overview summary and direct keyboard actions.
func (model Model) homeView() string {
	currency := "USD"
	if model.application != nil {
		currency = model.application.Config.Currency
	}
	var hours float64
	var total int64
	for _, entry := range model.overviewEntries {
		hours += entry.Hours
		total += entry.LineTotalCents()
	}
	lines := []string{accentStyle.Render("Overview") + mutedStyle.Render("  ·  "+model.now.Format("Mon, Jan 2, 2006"))}
	lines = append(lines, fmt.Sprintf("  %s %s    %s %s", valueStyle.Render("TODAY"), successStyle.Render(formatInterval(time.Duration(hours*float64(time.Hour)))), valueStyle.Render("UNBILLED"), successStyle.Render(currency+" "+domain.FormatMoney(total))))
	lines = append(lines, mutedStyle.Render("Recent work"))
	if len(model.overviewEntries) == 0 {
		lines = append(lines, mutedStyle.Render("  No time entries today."))
	} else {
		start := 0
		if len(model.overviewEntries) > 2 {
			start = len(model.overviewEntries) - 2
		}
		for _, entry := range model.overviewEntries[start:] {
			lines = append(lines, fmt.Sprintf("  %-8s %-28s %s %s", entry.StartAt.Local().Format("15:04"), truncate(entry.Description, 28), entry.Currency, domain.FormatMoney(entry.LineTotalCents())))
		}
	}
	lines = append(lines, accentStyle.Render("Quick actions"))
	labels := []string{"Add Time", "Time Entries", "Build Invoice", "Invoices", "Presets", "Settings", "Quit"}
	descriptions := []string{"Record a billable interval", "Review, edit, or remove work", "Select work and create a PDF", "Browse and export past invoices", "Manage reusable rates and descriptions", "Update invoice defaults", "Close ez-invoice"}
	for index, label := range labels {
		line := "  " + label
		if index == model.cursor {
			line = selectedStyle.Render("› " + label)
		}
		lines = append(lines, line)
	}
	if model.cursor >= 0 && model.cursor < len(descriptions) {
		lines = append(lines, mutedStyle.Render(descriptions[model.cursor]))
	}
	return strings.Join(lines, "\n")
}

// menuView renders a selected menu item and its contextual description.
func (model Model) menuView(title string, labels, descriptions []string) string {
	lines := []string{accentStyle.Render(title), ""}
	for index, label := range labels {
		line := "  " + label
		if index == model.cursor {
			line = selectedStyle.Render("› " + label)
		}
		lines = append(lines, line)
	}
	if model.cursor >= 0 && model.cursor < len(descriptions) {
		lines = append(lines, "", mutedStyle.Render(descriptions[model.cursor]))
	}
	return strings.Join(lines, "\n")
}

// formView renders aligned labels and selected values.
func (model Model) formView() string {
	titles := map[screen]string{
		screenTimeForm: "Time Entry", screenInvoiceRange: "Choose a date range", screenInvoiceMetadata: "Invoice details",
		screenRateForm: "Rate Preset", screenDescriptionForm: "Description Preset", screenRecipientForm: "Recipient", screenContactForm: "Contact",
	}
	if model.screen == screenSettingsForm {
		titles[screenSettingsForm] = "Sender"
		if model.settingsDefaults {
			titles[screenSettingsForm] = "Invoice Defaults"
		}
	}
	title := titles[model.screen]
	if model.screen == screenInvoiceRange || model.screen == screenInvoiceEntries || model.screen == screenInvoiceMetadata || model.screen == screenInvoiceReview {
		title += "  ·  Dates › Entries › Details › Review"
	}
	lines := []string{accentStyle.Render(title), ""}
	for index, item := range model.fields {
		value := item.displayValue()
		if item.kind == fieldAction {
			value = item.label
		}
		if index == model.cursor && model.editing {
			value = insertCursor(item.value, model.editAt)
		}
		line := fmt.Sprintf("%-20s %s", item.label, valueStyle.Render(value))
		if item.kind == fieldAction {
			line = "  " + value
		}
		if index == model.cursor {
			line = selectedStyle.Render("› " + line)
		}
		lines = append(lines, line)
		if model.screen == screenTimeForm && index == timeEndPeriodField {
			total := mutedStyle.Render("Enter a valid start and end time")
			if start, end, err := model.timeFormInterval(); err == nil {
				total = successStyle.Render(formatInterval(end.Sub(start)))
			}
			lines = append(lines, fmt.Sprintf("%-20s %s", "Total Time", total))
		}
	}
	return strings.Join(lines, "\n")
}

// timeConfirmationView summarizes the exact interval before it is persisted.
func (model Model) timeConfirmationView() string {
	lines := []string{
		accentStyle.Render("Confirm Time Entry"),
		"",
		fmt.Sprintf("%-12s %s", "Start", valueStyle.Render(model.timeStartAt.Format("Mon, Jan 2, 2006 at 3:04 PM"))),
		fmt.Sprintf("%-12s %s", "End", valueStyle.Render(model.timeEndAt.Format("Mon, Jan 2, 2006 at 3:04 PM"))),
		fmt.Sprintf("%-12s %s", "Total Time", successStyle.Render(formatInterval(model.timeEndAt.Sub(model.timeStartAt)))),
		"",
		mutedStyle.Render("Save this billable interval?"),
		"",
	}
	lines = append(lines, model.actionLines([]string{"Save Entry", "Go Back"})...)
	return strings.Join(lines, "\n")
}

// insertCursor adds a visible caret at the editor cursor.
func insertCursor(value string, at int) string {
	runes := []rune(value)
	if at < 0 {
		at = 0
	}
	if at > len(runes) {
		at = len(runes)
	}
	runes = append(runes, 0)
	copy(runes[at+1:], runes[at:])
	runes[at] = '█'
	return string(runes)
}

// timeEntriesView renders range controls, Add, and matching rows.
func (model Model) timeEntriesView() string {
	lines := []string{accentStyle.Render("Time Entries"), ""}
	for index, item := range model.fields {
		line := fmt.Sprintf("%-6s %s", item.label, item.displayValue())
		if index == model.cursor {
			line = selectedStyle.Render("› " + line)
		}
		lines = append(lines, line)
	}
	add := "  + Add Time"
	if model.cursor == 2 {
		add = selectedStyle.Render("› + Add Time")
	}
	lines = append(lines, add, "", mutedStyle.Render("    DATE        TIME         HOURS  DESCRIPTION          TOTAL"))
	if len(model.entries) == 0 {
		lines = append(lines, mutedStyle.Render("    No entries in this range."))
	}
	selected := model.cursor - 3
	start, end := model.listWindow(len(model.entries), selected, 18)
	for index := start; index < end; index++ {
		entry := model.entries[index]
		line := fmt.Sprintf("%-3d %-10s %s–%s %5.2f  %-18s %s %s", entry.ID, entry.StartAt.Local().Format("2006-01-02"),
			entry.StartAt.Local().Format("15:04"), entry.EndAt.Local().Format("15:04"), entry.Hours, truncate(entry.Description, 18), entry.Currency, domain.FormatMoney(entry.LineTotalCents()))
		if model.cursor == index+3 {
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
		valueStyle.Render(fmt.Sprintf("  Total%41s %s %s", "", model.application.Config.Currency, domain.FormatMoney(model.invoicePreview.TotalCents))), "")
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

// actionLines renders simple action rows using the active cursor.
func (model Model) actionLines(labels []string) []string {
	lines := make([]string, len(labels))
	for index, label := range labels {
		lines[index] = "  " + label
		if model.cursor == index {
			lines[index] = selectedStyle.Render("› " + label)
		}
	}
	return lines
}

// ratesView renders all rate presets and active state.
func (model Model) ratesView() string {
	presets, err := model.application.Store.ListRatePresets(context.Background(), true)
	if err != nil {
		return errorStyle.Render(err.Error())
	}
	lines := []string{accentStyle.Render("Rate Presets"), ""}
	add := "  + Add Rate"
	if model.cursor == 0 {
		add = selectedStyle.Render("› + Add Rate")
	}
	lines = append(lines, add, "", mutedStyle.Render("    LABEL                         RATE          STATE"))
	start, end := model.listWindow(len(presets), model.cursor-1, 16)
	for index := start; index < end; index++ {
		preset := presets[index]
		state := "inactive"
		if preset.Active {
			state = "active"
		}
		line := fmt.Sprintf("%-29s %s %9s  %s", truncate(preset.Label, 29), preset.Currency, domain.FormatMoney(preset.AmountCents), state)
		if model.cursor == index+1 {
			line = selectedStyle.Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if len(presets) == 0 {
		lines = append(lines, mutedStyle.Render("    No rate presets yet."))
	}
	if actions := model.inlineActionView(); actions != "" {
		lines = append(lines, actions)
	}
	return strings.Join(lines, "\n")
}

// descriptionsView renders all description presets and active state.
func (model Model) descriptionsView() string {
	presets, err := model.application.Store.ListDescriptionPresets(context.Background(), true)
	if err != nil {
		return errorStyle.Render(err.Error())
	}
	lines := []string{accentStyle.Render("Description Presets"), ""}
	add := "  + Add Description"
	if model.cursor == 0 {
		add = selectedStyle.Render("› + Add Description")
	}
	lines = append(lines, add, "")
	start, end := model.listWindow(len(presets), model.cursor-1, 15)
	for index := start; index < end; index++ {
		preset := presets[index]
		state := "inactive"
		if preset.Active {
			state = "active"
		}
		line := fmt.Sprintf("%-52s %s", truncate(preset.Label, 52), state)
		if model.cursor == index+1 {
			line = selectedStyle.Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if len(presets) == 0 {
		lines = append(lines, mutedStyle.Render("  No description presets yet."))
	}
	if actions := model.inlineActionView(); actions != "" {
		lines = append(lines, actions)
	}
	return strings.Join(lines, "\n")
}

// presetActionsView changes its second action between disable and restore.
func (model Model) presetActionsView(rate bool) string {
	title, active := "Description Preset", false
	if rate {
		title = "Rate Preset"
		if preset, err := model.findRate(model.selectedRate); err == nil {
			active = preset.Active
		}
	} else if preset, err := model.findDescription(model.selectedDescription); err == nil {
		active = preset.Active
	}
	toggle := "Restore"
	description := "Make this preset available again"
	if active {
		toggle = "Disable"
		description = "Hide it from new time entries"
	}
	return model.menuView(title, []string{"Edit", toggle, "Back"}, []string{"Change this preset", description, "Return to the list"})
}

// recipientsView renders Add and every configured invoice destination.
func (model Model) recipientsView() string {
	lines := []string{accentStyle.Render("Recipients"), ""}
	add := "  + Add Recipient"
	if model.cursor == 0 {
		add = selectedStyle.Render("› + Add Recipient")
	}
	lines = append(lines, add, "")
	start, end := model.listWindow(len(model.application.Config.Recipients), model.cursor-1, 15)
	for index := start; index < end; index++ {
		recipient := model.application.Config.Recipients[index]
		line := fmt.Sprintf("%-30s %s", truncate(recipient.CompanyName, 30), recipient.Address)
		if model.cursor == index+1 {
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

// contactsView renders Add and every configured invoice contact.
func (model Model) contactsView() string {
	lines := []string{accentStyle.Render("Contacts"), ""}
	add := "  + Add Contact"
	if model.cursor == 0 {
		add = selectedStyle.Render("› + Add Contact")
	}
	lines = append(lines, add, "")
	if len(model.application.Config.Contacts) == 0 {
		lines = append(lines, mutedStyle.Render("  No contacts yet."))
	}
	start, end := model.listWindow(len(model.application.Config.Contacts), model.cursor-1, 15)
	for index := start; index < end; index++ {
		contact := model.application.Config.Contacts[index]
		line := fmt.Sprintf("%-30s %s", truncate(contact.Name, 30), contact.Email)
		if model.cursor == index+1 {
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

// helpText reports only keys that apply to the active mode.
func (model Model) helpText() string {
	if model.editing {
		return "Type to edit  ·  ←/→ move cursor  ·  Enter accept  ·  Esc cancel"
	}
	if model.actionMode {
		return "←/→ choose action  ·  Enter activate  ·  Esc close  ·  Ctrl+C quit"
	}
	if model.screen == screenHome {
		return "←/→ areas  ·  ↑/↓ quick actions  ·  Enter open  ·  Ctrl+C quit"
	}
	if model.screen == screenTimeEntries && model.cursor < 2 {
		return "←/→ adjust date  ·  ↑/↓ rows  ·  Enter edit  ·  Esc back  ·  Ctrl+C quit"
	}
	if model.screen == screenTimeForm || model.screen == screenInvoiceRange || model.screen == screenInvoiceMetadata || model.screen == screenRateForm || model.screen == screenDescriptionForm || model.screen == screenSettingsForm || model.screen == screenRecipientForm || model.screen == screenContactForm {
		return "↑/↓ fields  ·  ←/→ adjust  ·  Enter type/save  ·  Esc back  ·  Ctrl+C quit"
	}
	if isRootScreen(model.screen) {
		return "←/→ areas  ·  ↑/↓ rows  ·  Enter select  ·  Esc overview  ·  Ctrl+C quit"
	}
	return "↑/↓ navigate  ·  ←/→ adjust  ·  Enter select/edit  ·  Esc back  ·  Ctrl+C quit"
}

// listWindow keeps the selected row inside the available terminal height.
func (model Model) listWindow(total, selected, fixedHeight int) (int, int) {
	if total == 0 || model.height == 0 {
		return 0, total
	}
	available := model.height - fixedHeight
	if available < 1 {
		available = 1
	}
	if available >= total {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	start := selected - available + 1
	if start < 0 {
		start = 0
	}
	return start, start + available
}

// truncate keeps table rows readable in ordinary terminal widths.
func truncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width < 2 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}
