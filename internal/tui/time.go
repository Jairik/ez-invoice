package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jairik/ez-invoice/internal/domain"
)

// openTimeForm loads defaults or an existing entry into the shared time form.
func (model *Model) openTimeForm(entry *domain.TimeEntry) {
	model.workspaceArea = workspaceTime
	model.push(screenTimeForm)
	descriptionChoices, rateChoices := model.loadPresetChoices()
	date, start, startPeriod := model.now.Format("2006-01-02"), "", "AM"
	endDate, end, endPeriod, notes := model.now.Format("2006-01-02"), "", "AM", ""
	model.timeCurrency = model.application.Config().Currency
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
	model.updateConfirmStrip(key, model.saveTimeEntry)
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
	model.updateActionStrip(key,
		func() {
			entry, err := model.application.Store.GetTimeEntry(context.Background(), model.selectedEntryID)
			if err != nil {
				model.setError(err)
				return
			}
			model.openTimeForm(&entry)
		},
		func() { model.push(screenConfirmTimeDelete) },
	)
}

// updateTimeDeleteConfirmation requires an explicit destructive choice.
func (model *Model) updateTimeDeleteConfirmation(key tea.KeyMsg) {
	model.updateDangerStrip(key, func() {
		if !model.runCLI([]string{"time", "delete", strconv.FormatInt(model.selectedEntryID, 10)}) {
			return
		}
		model.screen = screenTimeEntries
		if len(model.stack) >= 2 {
			model.stack = model.stack[:len(model.stack)-2]
		}
		model.cursor = 2
		model.makeTimeEntryRangeFields()
		model.loadTimeEntries()
		model.status = "Time entry deleted"
	})
}

// homeView renders the overview summary and direct keyboard actions.
func (model Model) homeView() string {
	currency := "USD"
	if model.application != nil {
		currency = model.application.Config().Currency
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
