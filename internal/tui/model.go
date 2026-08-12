// Package tui provides the menu-driven Bubble Tea interface.
package tui

import (
	"context"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Jairik/ez-invoice/internal/app"
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
		model.updateForm(key, func() { model.savePreset(true) })
	case screenRateActions:
		model.updatePresetActions(key, true)
	case screenConfirmRateToggle:
		model.updatePresetToggleConfirmation(key, true)
	case screenDescriptions:
		model.updateDescriptions(key)
	case screenDescriptionForm:
		model.updateForm(key, func() { model.savePreset(false) })
	case screenDescriptionActions:
		model.updatePresetActions(key, false)
	case screenConfirmDescriptionToggle:
		model.updatePresetToggleConfirmation(key, false)
	case screenSettings:
		model.updateSettings(key)
	case screenSettingsForm:
		model.updateForm(key, model.saveSettingsForm)
	case screenRecipients:
		model.updateRecipients(key)
	case screenRecipientForm:
		model.updateForm(key, model.saveRecipient)
	case screenRecipientActions:
		model.updateProfileActions(key, true)
	case screenConfirmRecipientDelete:
		model.updateProfileDeleteConfirmation(key, true)
	case screenContacts:
		model.updateContacts(key)
	case screenContactForm:
		model.updateForm(key, model.saveContact)
	case screenContactActions:
		model.updateProfileActions(key, false)
	case screenConfirmContactDelete:
		model.updateProfileDeleteConfirmation(key, false)
	}
	return model, nil
}

// updateActionStrip drives the standard three-item action strip.
func (model *Model) updateActionStrip(key tea.KeyMsg, edit, toggle func()) {
	model.moveCursor(key, 3)
	if key.Type != tea.KeyEnter {
		return
	}
	switch model.cursor {
	case 0:
		edit()
	case 1:
		toggle()
	case 2:
		model.back()
	}
}

// updateConfirmStrip drives two-item screens whose first item acts.
func (model *Model) updateConfirmStrip(key tea.KeyMsg, confirm func()) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 1 {
		model.back()
		return
	}
	confirm()
}

// updateDangerStrip drives two-item confirmations whose second item acts.
func (model *Model) updateDangerStrip(key tea.KeyMsg, confirm func()) {
	model.moveCursor(key, 2)
	if key.Type != tea.KeyEnter {
		return
	}
	if model.cursor == 0 {
		model.back()
		return
	}
	confirm()
}

// updateSimpleMenu applies shared movement to short submenus.
func (model *Model) updateSimpleMenu(key tea.KeyMsg, count int, open func()) {
	model.moveCursor(key, count)
	if key.Type == tea.KeyEnter {
		open()
	}
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
			model.openPresetForm(true, preset.ID, preset.Label, domain.FormatMoney(preset.AmountCents), preset.Currency)
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
		model.openPresetForm(false, preset.ID, preset.Label, "", "")
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
	if model.actionCursor == 0 {
		model.actionMode = false
		if recipient {
			model.openRecipientForm(model.selectedConfigIndex)
		} else {
			model.openContactForm(model.selectedConfigIndex)
		}
		return
	}
	model.actionMode = false
	if recipient {
		model.push(screenConfirmRecipientDelete)
	} else {
		model.push(screenConfirmContactDelete)
	}
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
	entries, err := model.application.Store.ListTimeEntries(context.Background(), from, from.AddDate(0, 0, 1), true)
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
