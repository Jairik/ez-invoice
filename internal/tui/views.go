package tui

import (
	"strings"
)

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
