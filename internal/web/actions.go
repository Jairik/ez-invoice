package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// redirectTo sends the browser back with a flash message.
func redirectTo(w http.ResponseWriter, r *http.Request, path string, ok bool, message string) {
	setFlash(w, ok, message)
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// createTime stores a new manual time entry.
func (s *Server) createTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	entry, err := s.parseTimeForm(r)
	if err != nil {
		redirectTo(w, r, "/time", false, err.Error())
		return
	}
	if _, err := s.app.Store.CreateTimeEntry(ctx, entry); err != nil {
		redirectTo(w, r, "/time", false, err.Error())
		return
	}
	redirectTo(w, r, "/time", true, "Time entry added")
}

// updateTime saves edits to an uninvoiced entry.
func (s *Server) updateTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	entry, err := s.app.Store.GetTimeEntry(ctx, id)
	if err != nil {
		redirectTo(w, r, "/time", false, err.Error())
		return
	}
	entry, err = s.parseTimeForm(r)
	if err != nil {
		redirectTo(w, r, "/time", false, err.Error())
		return
	}
	entry.ID = id
	if _, err := s.app.Store.UpdateTimeEntry(ctx, entry); err != nil {
		redirectTo(w, r, "/time", false, err.Error())
		return
	}
	redirectTo(w, r, "/time", true, "Time entry updated")
}

// deleteTime removes an uninvoiced entry.
func (s *Server) deleteTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.app.Store.DeleteTimeEntry(ctx, id); err != nil {
		redirectTo(w, r, "/time", false, err.Error())
		return
	}
	redirectTo(w, r, "/time", true, "Time entry deleted")
}

// parseTimeForm reads one entry form, preserving preset links.
func (s *Server) parseTimeForm(r *http.Request) (domain.TimeEntry, error) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		return domain.TimeEntry{}, err
	}
	entry := domain.TimeEntry{
		Description: strings.TrimSpace(r.PostFormValue("description")),
		Currency:    strings.TrimSpace(r.PostFormValue("currency")),
		Notes:       r.PostFormValue("notes"),
	}
	if entry.Currency == "" {
		entry.Currency = s.app.Config().Currency
	}
	start, err := parseDateTimeLocal(r.PostFormValue("start"))
	if err != nil {
		return domain.TimeEntry{}, fmt.Errorf("start: %w", err)
	}
	end, err := parseDateTimeLocal(r.PostFormValue("end"))
	if err != nil {
		return domain.TimeEntry{}, fmt.Errorf("end: %w", err)
	}
	entry.StartAt, entry.EndAt = start, end

	if descriptionPreset := r.PostFormValue("description_preset"); descriptionPreset != "" && descriptionPreset != "custom" {
		id, err := strconv.ParseInt(descriptionPreset, 10, 64)
		if err != nil {
			return domain.TimeEntry{}, errors.New("invalid description preset")
		}
		presets, err := s.app.Store.ListDescriptionPresets(ctx, false)
		if err != nil {
			return domain.TimeEntry{}, err
		}
		found := false
		for _, preset := range presets {
			if preset.ID == id {
				entry.Description, entry.DescriptionPresetID = preset.Label, &preset.ID
				found = true
				break
			}
		}
		if !found {
			return domain.TimeEntry{}, fmt.Errorf("active description preset %d not found", id)
		}
	}
	if entry.Description == "" {
		return domain.TimeEntry{}, errors.New("description is required")
	}

	if ratePreset := r.PostFormValue("rate_preset"); ratePreset != "" && ratePreset != "custom" {
		id, err := strconv.ParseInt(ratePreset, 10, 64)
		if err != nil {
			return domain.TimeEntry{}, errors.New("invalid rate preset")
		}
		presets, err := s.app.Store.ListRatePresets(ctx, false)
		if err != nil {
			return domain.TimeEntry{}, err
		}
		found := false
		for _, preset := range presets {
			if preset.ID == id {
				entry.RateAmountCents, entry.Currency, entry.RatePresetID = preset.AmountCents, preset.Currency, &preset.ID
				found = true
				break
			}
		}
		if !found {
			return domain.TimeEntry{}, fmt.Errorf("active rate preset %d not found", id)
		}
	} else {
		amount, err := domain.ParseMoney(r.PostFormValue("rate"))
		if err != nil || amount < 0 {
			return domain.TimeEntry{}, errors.New("rate must be a non-negative decimal")
		}
		entry.RateAmountCents = amount
	}
	return entry, nil
}

// parseDateTimeLocal parses an input datetime in the local timezone.
func parseDateTimeLocal(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, errors.New("a datetime is required")
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", value, time.Local)
	if err != nil {
		return time.Time{}, errors.New("use a valid local datetime")
	}
	return parsed, nil
}

// createRate stores a new rate preset.
func (s *Server) createRate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	preset, err := s.parseRateForm(r)
	if err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	preset.Active = true
	if _, err := s.app.Store.CreateRatePreset(ctx, preset); err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	redirectTo(w, r, "/presets", true, "Rate added")
}

// updateRate saves edits to a rate preset.
func (s *Server) updateRate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	presets, err := s.app.Store.ListRatePresets(ctx, true)
	if err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	for _, preset := range presets {
		if preset.ID != id {
			continue
		}
		updated, err := s.parseRateForm(r)
		if err != nil {
			redirectTo(w, r, "/presets", false, err.Error())
			return
		}
		updated.ID, updated.Active = id, preset.Active
		if _, err := s.app.Store.UpdateRatePreset(ctx, updated); err != nil {
			redirectTo(w, r, "/presets", false, err.Error())
			return
		}
		redirectTo(w, r, "/presets", true, "Rate updated")
		return
	}
	redirectTo(w, r, "/presets", false, "rate preset not found")
}

// toggleRate activates or deactivates a rate.
func (s *Server) toggleRate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.app.Store.SetRatePresetActive(ctx, id, active); err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	message := "Rate deactivated"
	if active {
		message = "Rate restored"
	}
	redirectTo(w, r, "/presets", true, message)
}

// parseRateForm reads label, amount, and currency.
func (s *Server) parseRateForm(r *http.Request) (domain.RatePreset, error) {
	if err := r.ParseForm(); err != nil {
		return domain.RatePreset{}, err
	}
	preset := domain.RatePreset{
		Label:    strings.TrimSpace(r.PostFormValue("label")),
		Currency: strings.TrimSpace(r.PostFormValue("currency")),
	}
	if preset.Currency == "" {
		preset.Currency = s.app.Config().Currency
	}
	amount, err := domain.ParseMoney(r.PostFormValue("amount"))
	if err != nil || amount < 0 {
		return domain.RatePreset{}, errors.New("rate amount must be a non-negative decimal")
	}
	preset.AmountCents = amount
	return preset, nil
}

// createDescription stores a new description preset.
func (s *Server) createDescription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	label := strings.TrimSpace(r.PostFormValue("label"))
	if label == "" {
		redirectTo(w, r, "/presets", false, "description label is required")
		return
	}
	if _, err := s.app.Store.CreateDescriptionPreset(ctx, domain.DescriptionPreset{Label: label, Active: true}); err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	redirectTo(w, r, "/presets", true, "Description added")
}

// updateDescription saves edits to a description preset.
func (s *Server) updateDescription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	presets, err := s.app.Store.ListDescriptionPresets(ctx, true)
	if err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	for _, preset := range presets {
		if preset.ID != id {
			continue
		}
		label := strings.TrimSpace(r.PostFormValue("label"))
		if label == "" {
			redirectTo(w, r, "/presets", false, "description label is required")
			return
		}
		preset.Label = label
		if _, err := s.app.Store.UpdateDescriptionPreset(ctx, preset); err != nil {
			redirectTo(w, r, "/presets", false, err.Error())
			return
		}
		redirectTo(w, r, "/presets", true, "Description updated")
		return
	}
	redirectTo(w, r, "/presets", false, "description preset not found")
}

// toggleDescription activates or deactivates a description.
func (s *Server) toggleDescription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.app.Store.SetDescriptionPresetActive(ctx, id, active); err != nil {
		redirectTo(w, r, "/presets", false, err.Error())
		return
	}
	message := "Description deactivated"
	if active {
		message = "Description restored"
	}
	redirectTo(w, r, "/presets", true, message)
}

// settingsView feeds the settings page.
type settingsView struct {
	Config configSnapshot
}

// settingsPage renders the editable configuration.
func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	view := settingsView{Config: configSnapshot(s.app.Config())}
	if err := s.renderPage(w, r, "settings.html", pageView{Title: "Settings", Active: "settings"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// saveSettings validates and persists the configuration form.
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectTo(w, r, "/settings", false, err.Error())
		return
	}
	cfg := s.parseSettingsForm(r)
	if err := config.Save(s.app.Paths.ConfigFile, cfg); err != nil {
		view := settingsView{Config: configSnapshot(cfg)}
		setFlash(w, false, err.Error())
		if renderErr := s.renderPage(w, r, "settings.html", pageView{Title: "Settings", Active: "settings"}, view); renderErr != nil {
			http.Error(w, renderErr.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.app.SetConfig(cfg)
	redirectTo(w, r, "/settings", true, "Settings saved")
}

// parseSettingsForm rebuilds the config from posted rows and new entries.
func (s *Server) parseSettingsForm(r *http.Request) config.Config {
	cfg := s.app.Config()
	cfg.Sender = config.Sender{
		FullName: strings.TrimSpace(r.PostFormValue("sender_name")),
		Address:  strings.TrimSpace(r.PostFormValue("sender_address")),
		Email:    strings.TrimSpace(r.PostFormValue("sender_email")),
	}
	companies := r.PostForm["recipient_company"]
	addresses := r.PostForm["recipient_address"]
	deletes := map[string]bool{}
	for _, value := range r.PostForm["recipient_delete"] {
		deletes[value] = true
	}
	cfg.Recipients = nil
	for index, company := range companies {
		if deletes[strconv.Itoa(index)] {
			continue
		}
		address := ""
		if index < len(addresses) {
			address = addresses[index]
		}
		cfg.Recipients = append(cfg.Recipients, config.Recipient{CompanyName: strings.TrimSpace(company), Address: strings.TrimSpace(address)})
	}
	names := r.PostForm["contact_name"]
	emails := r.PostForm["contact_email"]
	contactDeletes := map[string]bool{}
	for _, value := range r.PostForm["contact_delete"] {
		contactDeletes[value] = true
	}
	cfg.Contacts = nil
	for index, name := range names {
		if contactDeletes[strconv.Itoa(index)] {
			continue
		}
		email := ""
		if index < len(emails) {
			email = emails[index]
		}
		cfg.Contacts = append(cfg.Contacts, config.Contact{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email)})
	}
	if company := strings.TrimSpace(r.PostFormValue("new_recipient_company")); company != "" {
		cfg.Recipients = append(cfg.Recipients, config.Recipient{
			CompanyName: company, Address: strings.TrimSpace(r.PostFormValue("new_recipient_address")),
		})
	}
	if name := strings.TrimSpace(r.PostFormValue("new_contact_name")); name != "" {
		cfg.Contacts = append(cfg.Contacts, config.Contact{
			Name: name, Email: strings.TrimSpace(r.PostFormValue("new_contact_email")),
		})
	}
	cfg.PayableTerms = strings.TrimSpace(r.PostFormValue("terms"))
	cfg.Currency = strings.TrimSpace(r.PostFormValue("currency"))
	cfg.LogoPath = strings.TrimSpace(r.PostFormValue("logo"))
	cfg.OutputDir = strings.TrimSpace(r.PostFormValue("output"))
	cfg.Notes = r.PostFormValue("notes")
	cfg.DefaultAdjustment = strings.TrimSpace(r.PostFormValue("adjustment"))
	return cfg
}
