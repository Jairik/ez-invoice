package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jairik/ez-invoice/internal/cli"
	"github.com/Jairik/ez-invoice/internal/config"
)

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
	cfg := model.application.Config()
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
	cfg := model.application.Config()
	if model.settingsDefaults {
		cfg.PayableTerms, cfg.Currency, cfg.LogoPath = model.fields[0].value, model.fields[1].value, model.fields[2].value
		cfg.OutputDir, cfg.Notes, cfg.DefaultAdjustment = model.fields[3].value, model.fields[4].value, model.fields[5].value
	} else {
		cfg.Sender.FullName, cfg.Sender.Address, cfg.Sender.Email = model.fields[senderNameField].value, model.fields[senderAddressField].value, model.fields[senderEmailField].value
	}
	if model.saveConfig(cfg, "Settings saved") {
		model.finishBack()
	}
}

// updateRecipients navigates Add and recipient profiles.
func (model *Model) updateRecipients(key tea.KeyMsg) {
	model.moveCursor(key, len(model.application.Config().Recipients)+1)
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
	if index >= 0 && index < len(model.application.Config().Recipients) {
		recipient = model.application.Config().Recipients[index]
	}
	model.fields = []field{
		{label: "Company", value: recipient.CompanyName, kind: fieldText},
		{label: "Address", value: recipient.Address, kind: fieldText},
		{label: "Save Recipient", kind: fieldAction},
	}
}

// saveRecipient adds or updates a recipient profile.
func (model *Model) saveRecipient() {
	cfg := model.application.Config()
	cfg.Recipients = append([]config.Recipient(nil), cfg.Recipients...)
	recipient := config.Recipient{CompanyName: model.fields[0].value, Address: model.fields[1].value}
	if model.selectedConfigIndex < 0 {
		cfg.Recipients = append(cfg.Recipients, recipient)
	} else {
		cfg.Recipients[model.selectedConfigIndex] = recipient
	}
	if model.saveConfig(cfg, "Recipient saved") {
		model.finishBack()
	}
}

// updateContacts navigates Add and contact profiles.
func (model *Model) updateContacts(key tea.KeyMsg) {
	model.moveCursor(key, len(model.application.Config().Contacts)+1)
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
	if index >= 0 && index < len(model.application.Config().Contacts) {
		contact = model.application.Config().Contacts[index]
	}
	model.fields = []field{
		{label: "Name", value: contact.Name, kind: fieldText},
		{label: "Email", value: contact.Email, kind: fieldText},
		{label: "Save Contact", kind: fieldAction},
	}
}

// saveContact adds or updates a contact profile.
func (model *Model) saveContact() {
	cfg := model.application.Config()
	cfg.Contacts = append([]config.Contact(nil), cfg.Contacts...)
	contact := config.Contact{Name: model.fields[0].value, Email: model.fields[1].value}
	if model.selectedConfigIndex < 0 {
		cfg.Contacts = append(cfg.Contacts, contact)
	} else {
		cfg.Contacts[model.selectedConfigIndex] = contact
	}
	if model.saveConfig(cfg, "Contact saved") {
		model.finishBack()
	}
}

// updateProfileActions edits or requests deletion of a configured profile.
func (model *Model) updateProfileActions(key tea.KeyMsg, recipient bool) {
	model.updateActionStrip(key,
		func() {
			if recipient {
				model.openRecipientForm(model.selectedConfigIndex)
			} else {
				model.openContactForm(model.selectedConfigIndex)
			}
		},
		func() {
			if recipient {
				model.push(screenConfirmRecipientDelete)
			} else {
				model.push(screenConfirmContactDelete)
			}
		},
	)
}

// updateProfileDeleteConfirmation protects profile removal.
func (model *Model) updateProfileDeleteConfirmation(key tea.KeyMsg, recipient bool) {
	model.updateDangerStrip(key, func() {
		cfg := model.application.Config()
		index := model.selectedConfigIndex
		if recipient {
			if len(cfg.Recipients) == 1 {
				model.setError(errors.New("at least one recipient profile is required"))
				return
			}
			cfg.Recipients = append([]config.Recipient(nil), cfg.Recipients...)
			cfg.Recipients = append(cfg.Recipients[:index], cfg.Recipients[index+1:]...)
			if model.saveConfig(cfg, "Recipient deleted") {
				model.leaveTwoScreens(screenRecipients, "Recipient deleted")
			}
			return
		}
		cfg.Contacts = append([]config.Contact(nil), cfg.Contacts...)
		cfg.Contacts = append(cfg.Contacts[:index], cfg.Contacts[index+1:]...)
		if model.saveConfig(cfg, "Contact deleted") {
			model.leaveTwoScreens(screenContacts, "Contact deleted")
		}
	})
}

// saveConfig persists one validated configuration snapshot.
func (model *Model) saveConfig(cfg config.Config, message string) bool {
	if err := config.Save(model.application.Paths.ConfigFile, cfg); err != nil {
		model.setError(err)
		return false
	}
	model.application.SetConfig(cfg)
	model.status, model.statusError = message, false
	return true
}

// finishBack returns to the previous screen while preserving a success message.
func (model *Model) finishBack() {
	message := model.status
	model.back()
	model.status = message
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

// recipientsView renders Add and every configured invoice destination.
func (model Model) recipientsView() string {
	recipients := model.application.Config().Recipients
	return model.listView("Recipients", "Add Recipient", "", len(recipients), 15, "", func(index int) string {
		recipient := recipients[index]
		return fmt.Sprintf("%-30s %s", truncate(recipient.CompanyName, 30), recipient.Address)
	})
}

// contactsView renders Add and every configured invoice contact.
func (model Model) contactsView() string {
	contacts := model.application.Config().Contacts
	empty := "No contacts yet."
	if len(contacts) > 0 {
		empty = ""
	}
	return model.listView("Contacts", "Add Contact", "", len(contacts), 15, empty, func(index int) string {
		contact := contacts[index]
		return fmt.Sprintf("%-30s %s", truncate(contact.Name, 30), contact.Email)
	})
}
