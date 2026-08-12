package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
	dateIndex := timeDateField
	if index == timeEndField {
		periodIndex = timeEndPeriodField
		dateIndex = timeEndDateField
	}
	period := model.fields[periodIndex].value
	parsed, err := time.Parse("03:04 PM", model.fields[index].value+" "+period)
	if err != nil {
		minute := model.now.Minute() / 15 * 15
		parsed = time.Date(0, 1, 1, model.now.Hour(), minute, 0, 0, time.UTC)
	}
	oldDay := parsed.Day()
	parsed = parsed.Add(time.Duration(direction) * 15 * time.Minute)
	if parsed.Day() != oldDay {
		model.adjustDateField(dateIndex, direction)
	}
	model.fields[index].value = parsed.Format("03:04")
	model.fields[periodIndex].value = parsed.Format("PM")
	model.fields[periodIndex].choiceAt = 0
	if model.fields[periodIndex].value == "PM" {
		model.fields[periodIndex].choiceAt = 1
	}
}

// adjustDateField moves a YYYY-MM-DD form value by whole days.
func (model *Model) adjustDateField(index, days int) {
	if index < 0 || index >= len(model.fields) {
		return
	}
	if parsed, err := time.ParseInLocation("2006-01-02", model.fields[index].value, time.Local); err == nil {
		model.fields[index].value = parsed.AddDate(0, 0, days).Format("2006-01-02")
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
