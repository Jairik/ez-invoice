package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jairik/ez-invoice/internal/domain"
)

// presetScreens maps the rate flag onto its four screen identifiers.
func presetScreens(rate bool) (list, form, actions, confirm screen) {
	if rate {
		return screenRates, screenRateForm, screenRateActions, screenConfirmRateToggle
	}
	return screenDescriptions, screenDescriptionForm, screenDescriptionActions, screenConfirmDescriptionToggle
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
		model.openPresetForm(true, 0, "", "", model.application.Config().Currency)
		return
	}
	model.selectedRate = presets[model.cursor-1].ID
	model.actionMode, model.actionCursor = true, 0
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
		model.openPresetForm(false, 0, "", "", "")
		return
	}
	model.selectedDescription = presets[model.cursor-1].ID
	model.actionMode, model.actionCursor = true, 0
}

// openPresetForm creates fields for adding or editing either preset type.
func (model *Model) openPresetForm(rate bool, presetID int64, label, amount, currency string) {
	model.workspaceArea = workspacePresets
	if !rate {
		model.push(screenDescriptionForm)
		model.selectedDescription = presetID
		model.fields = []field{{label: "Description", value: label, kind: fieldText}, {label: "Save Description", kind: fieldAction}}
		return
	}
	model.push(screenRateForm)
	model.selectedRate = presetID
	model.fields = []field{
		{label: "Label", value: label, kind: fieldText},
		{label: "Amount", value: amount, placeholder: "0.00", kind: fieldText},
		{label: "Currency", value: currency, kind: fieldText},
		{label: "Save Rate", kind: fieldAction},
	}
}

// savePreset persists a new or edited preset through the shared CLI.
func (model *Model) savePreset(rate bool) {
	if !rate {
		args := []string{"description", "add", model.fields[descriptionLabelField].value}
		if model.selectedDescription != 0 {
			args = []string{"description", "update", strconv.FormatInt(model.selectedDescription, 10), model.fields[descriptionLabelField].value}
		}
		if model.runCLI(args) {
			model.finishBack()
		}
		return
	}
	args := []string{"rate", "add", model.fields[rateLabelField].value, model.fields[rateAmountField].value, model.fields[rateCurrencyField].value}
	if model.selectedRate != 0 {
		args = []string{"rate", "update", strconv.FormatInt(model.selectedRate, 10), model.fields[rateLabelField].value, model.fields[rateAmountField].value, model.fields[rateCurrencyField].value}
	}
	if model.runCLI(args) {
		model.finishBack()
	}
}

// updatePresetActions edits, disables, or restores the selected preset.
func (model *Model) updatePresetActions(key tea.KeyMsg, rate bool) {
	model.updateActionStrip(key,
		func() {
			if rate {
				preset, err := model.findRate(model.selectedRate)
				if err != nil {
					model.setError(err)
					return
				}
				model.openPresetForm(true, preset.ID, preset.Label, domain.FormatMoney(preset.AmountCents), preset.Currency)
				return
			}
			preset, err := model.findDescription(model.selectedDescription)
			if err != nil {
				model.setError(err)
				return
			}
			model.openPresetForm(false, preset.ID, preset.Label, "", "")
		},
		func() {
			if rate {
				preset, err := model.findRate(model.selectedRate)
				if err != nil {
					model.setError(err)
					return
				}
				if !preset.Active {
					if model.runCLI([]string{"rate", "restore", strconv.FormatInt(preset.ID, 10)}) {
						model.back()
						model.status = "Rate restored"
					}
					return
				}
				model.push(screenConfirmRateToggle)
				return
			}
			preset, err := model.findDescription(model.selectedDescription)
			if err != nil {
				model.setError(err)
				return
			}
			if !preset.Active {
				if model.runCLI([]string{"description", "restore", strconv.FormatInt(preset.ID, 10)}) {
					model.back()
					model.status = "Description restored"
				}
				return
			}
			model.push(screenConfirmDescriptionToggle)
		},
	)
}

// updatePresetToggleConfirmation requires confirmation before disabling a preset.
func (model *Model) updatePresetToggleConfirmation(key tea.KeyMsg, rate bool) {
	model.updateDangerStrip(key, func() {
		command := "description"
		selected := model.selectedDescription
		message := "Description disabled"
		target := screenDescriptions
		if rate {
			command, selected, message, target = "rate", model.selectedRate, "Rate disabled", screenRates
		}
		if model.runCLI([]string{command, "delete", strconv.FormatInt(selected, 10)}) {
			model.leaveTwoScreens(target, message)
		}
	})
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

// listView renders the Add row, item rows, and inline actions for a list screen.
func (model Model) listView(title, addLabel, header string, count, window int, empty string, row func(index int) string) string {
	lines := []string{accentStyle.Render(title), ""}
	add := "  + " + addLabel
	if model.cursor == 0 {
		add = selectedStyle.Render("› + " + addLabel)
	}
	lines = append(lines, add, "")
	if header != "" {
		lines = append(lines, mutedStyle.Render(header))
	}
	start, end := model.listWindow(count, model.cursor-1, window)
	for index := start; index < end; index++ {
		line := row(index)
		if model.cursor == index+1 {
			line = selectedStyle.Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if count == 0 && empty != "" {
		lines = append(lines, mutedStyle.Render("  "+empty))
	}
	if actions := model.inlineActionView(); actions != "" {
		lines = append(lines, actions)
	}
	return strings.Join(lines, "\n")
}

// ratesView renders all rate presets and active state.
func (model Model) ratesView() string {
	presets, err := model.application.Store.ListRatePresets(context.Background(), true)
	if err != nil {
		return errorStyle.Render(err.Error())
	}
	return model.listView("Rate Presets", "Add Rate", "    LABEL                         RATE          STATE", len(presets), 16, "No rate presets yet.", func(index int) string {
		preset := presets[index]
		state := "inactive"
		if preset.Active {
			state = "active"
		}
		return fmt.Sprintf("%-29s %s %9s  %s", truncate(preset.Label, 29), preset.Currency, domain.FormatMoney(preset.AmountCents), state)
	})
}

// descriptionsView renders all description presets and active state.
func (model Model) descriptionsView() string {
	presets, err := model.application.Store.ListDescriptionPresets(context.Background(), true)
	if err != nil {
		return errorStyle.Render(err.Error())
	}
	return model.listView("Description Presets", "Add Description", "", len(presets), 15, "No description presets yet.", func(index int) string {
		preset := presets[index]
		state := "inactive"
		if preset.Active {
			state = "active"
		}
		return fmt.Sprintf("%-52s %s", truncate(preset.Label, 52), state)
	})
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
