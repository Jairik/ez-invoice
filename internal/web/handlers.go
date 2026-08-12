package web

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// dashboardView feeds the overview page.
type dashboardView struct {
	TodayHours    float64
	TodayValue    int64
	MonthHours    float64
	MonthValue    int64
	Unbilled      int64
	UnbilledHours float64
	UnbilledCount int
	Invoiced      int64
	InvoiceCount  int
	Bars          []monthBar
	Recent        []domain.TimeEntry
	Invoices      []domain.Invoice
	Today         string
	MonthName     string
	DaysLeft      int
	DataDir       string
	DefaultRate   string
}

// monthBar is one column of the six-month hours chart.
type monthBar struct {
	Label string
	Hours float64
	Width int
}

// saturatingAdd clamps at MaxInt64 instead of wrapping negative for display sums.
func saturatingAdd(total, value int64) int64 {
	if value > 0 && total > math.MaxInt64-value {
		return math.MaxInt64
	}
	return total + value
}

// dashboard renders the overview workspace.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	view := dashboardView{}
	view.Today = now.Format("Monday 2 January 2006")
	view.MonthName = now.Format("January")
	view.DataDir = s.app.Paths.DataDir
	// Days remaining in the current period, counting today as still open.
	view.DaysLeft = int(startOfMonth.AddDate(0, 1, 0).Sub(startOfDay).Hours() / 24)

	todayEntries, err := s.app.Store.ListTimeEntries(ctx, startOfDay, startOfDay.AddDate(0, 0, 1), true)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Overview", Active: "overview"}, err)
		return
	}
	monthEntries, err := s.app.Store.ListTimeEntries(ctx, startOfMonth, startOfMonth.AddDate(0, 1, 0), true)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Overview", Active: "overview"}, err)
		return
	}
	unbilledEntries, err := s.app.Store.ListTimeEntries(ctx, now.AddDate(-5, 0, 0), now.AddDate(0, 0, 5), true)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Overview", Active: "overview"}, err)
		return
	}
	invoices, err := s.app.Store.ListInvoices(ctx)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Overview", Active: "overview"}, err)
		return
	}
	recent, err := s.app.Store.ListTimeEntries(ctx, now.AddDate(0, 0, -30), now.AddDate(0, 0, 1), false)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Overview", Active: "overview"}, err)
		return
	}

	for _, entry := range todayEntries {
		view.TodayHours += entry.Hours
		view.TodayValue = saturatingAdd(view.TodayValue, entry.LineTotalCents())
	}
	for _, entry := range monthEntries {
		view.MonthHours += entry.Hours
		view.MonthValue = saturatingAdd(view.MonthValue, entry.LineTotalCents())
	}
	for _, entry := range unbilledEntries {
		view.Unbilled = saturatingAdd(view.Unbilled, entry.LineTotalCents())
		view.UnbilledHours += entry.Hours
		view.UnbilledCount++
	}
	for _, invoice := range invoices {
		view.Invoiced = saturatingAdd(view.Invoiced, invoice.TotalCents)
	}
	view.InvoiceCount = len(invoices)
	// The first active rate preset is what a new entry defaults to.
	if rates, err := s.app.Store.ListRatePresets(ctx, true); err == nil && len(rates) > 0 {
		view.DefaultRate = rates[0].Currency + " " + domain.FormatMoney(rates[0].AmountCents)
	}
	view.Bars = monthBars(now, recent)
	view.Recent = lastEntries(recent, 5)
	view.Invoices = invoices
	if len(invoices) > 4 {
		view.Invoices = invoices[:4]
	}

	if err := s.renderPage(w, r, "dashboard.html", pageView{Title: "Overview", Active: "overview"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// monthBars sums hours per calendar month for the last six months.
func monthBars(now time.Time, entries []domain.TimeEntry) []monthBar {
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	bars := make([]monthBar, 6)
	for index := range bars {
		month := current.AddDate(0, index-5, 0)
		bars[index] = monthBar{Label: month.Format("Jan")}
	}
	for _, entry := range entries {
		start := entry.StartAt.Local()
		for index := range bars {
			monthStart := current.AddDate(0, index-5, 0)
			if start.Year() == monthStart.Year() && start.Month() == monthStart.Month() {
				bars[index].Hours += entry.Hours
				break
			}
		}
	}
	maxHours := 0.0
	for _, bar := range bars {
		if bar.Hours > maxHours {
			maxHours = bar.Hours
		}
	}
	if maxHours == 0 {
		return bars
	}
	for index := range bars {
		bars[index].Width = int(bars[index].Hours / maxHours * 100)
	}
	return bars
}

// lastEntries returns the most recent entries newest first.
func lastEntries(entries []domain.TimeEntry, count int) []domain.TimeEntry {
	result := make([]domain.TimeEntry, 0, count)
	for index := len(entries) - 1; index >= 0 && len(result) < count; index-- {
		result = append(result, entries[index])
	}
	return result
}

// timeView feeds the time entries workspace.
type timeView struct {
	From       string
	To         string
	All        bool
	Entries    []domain.TimeEntry
	DescPreset []domain.DescriptionPreset
	RatePreset []domain.RatePreset
	TotalHours float64
	TotalValue int64
}

// timePage lists entries in a selectable date range.
func (s *Server) timePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	from, to, err := parseDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), now)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Time", Active: "time"}, err)
		return
	}
	all := r.URL.Query().Get("all") == "1"
	entries, err := s.app.Store.ListTimeEntries(ctx, from, to, !all)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Time", Active: "time"}, err)
		return
	}
	descPresets, err := s.app.Store.ListDescriptionPresets(ctx, false)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Time", Active: "time"}, err)
		return
	}
	ratePresets, err := s.app.Store.ListRatePresets(ctx, false)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Time", Active: "time"}, err)
		return
	}
	view := timeView{
		From:       from.Format("2006-01-02"),
		To:         to.Add(-time.Second).Format("2006-01-02"),
		All:        all,
		Entries:    entries,
		DescPreset: descPresets,
		RatePreset: ratePresets,
	}
	for _, entry := range entries {
		view.TotalHours += entry.Hours
		if entry.InvoiceID == nil {
			view.TotalValue += entry.LineTotalCents()
		}
	}
	if err := s.renderPage(w, r, "time.html", pageView{Title: "Time", Active: "time"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// presetsView feeds the presets workspace.
type presetsView struct {
	Rates        []domain.RatePreset
	Descriptions []domain.DescriptionPreset
}

// presetsPage lists reusable rates and descriptions.
func (s *Server) presetsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rates, err := s.app.Store.ListRatePresets(ctx, true)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Presets", Active: "presets"}, err)
		return
	}
	descriptions, err := s.app.Store.ListDescriptionPresets(ctx, true)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Presets", Active: "presets"}, err)
		return
	}
	view := presetsView{

		Rates:        rates,
		Descriptions: descriptions,
	}
	if err := s.renderPage(w, r, "presets.html", pageView{Title: "Presets", Active: "presets"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// invoiceRow pairs one snapshot with the state of its exported file, so the
// history can tell a saved PDF apart from one whose file has since gone.
type invoiceRow struct {
	domain.Invoice
	PDFOnDisk bool
}

// invoicesView feeds the invoice history page.
type invoicesView struct {
	Invoices      []invoiceRow
	TotalInvoiced int64
}

// pdfOnDisk reports whether a recorded PDF path still resolves to a file.
func pdfOnDisk(path string) (int64, bool) {
	if path == "" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0, false
	}
	return info.Size(), true
}

// invoicesPage lists finalized invoice snapshots.
func (s *Server) invoicesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	invoices, err := s.app.Store.ListInvoices(ctx)
	if err != nil {
		renderError(w, r, s, pageView{Title: "Invoices", Active: "invoices"}, err)
		return
	}
	view := invoicesView{}
	for _, invoice := range invoices {
		_, onDisk := pdfOnDisk(invoice.PDFPath)
		view.Invoices = append(view.Invoices, invoiceRow{Invoice: invoice, PDFOnDisk: onDisk})
		view.TotalInvoiced += invoice.TotalCents
	}
	if err := s.renderPage(w, r, "invoices.html", pageView{Title: "Invoices", Active: "invoices"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// invoiceDetailView feeds one snapshot page.
type invoiceDetailView struct {
	Invoice   domain.Invoice
	PDFOnDisk bool
	PDFSize   int64
	Entries   int
}

// invoiceDetail shows one finalized invoice and its actions.
func (s *Server) invoiceDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	invoice, err := s.app.Store.GetInvoice(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		renderError(w, r, s, pageView{Title: "Invoices", Active: "invoices"}, err)
		return
	}
	size, onDisk := pdfOnDisk(invoice.PDFPath)
	view := invoiceDetailView{
		Invoice:   invoice,
		PDFOnDisk: onDisk,
		PDFSize:   size,
		Entries:   len(invoice.LineItems),
	}
	if err := s.renderPage(w, r, "invoice_detail.html", pageView{Title: "Invoice " + invoice.DisplayNumber(), Active: "invoices"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderError renders a simple error page with the reported message.
func renderError(w http.ResponseWriter, r *http.Request, s *Server, view pageView, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	payload := struct {
		Title   string
		Active  string
		Flash   string
		FlashOK bool
		Config  config.Config
		Data    any
	}{Title: "Something went wrong", Active: view.Active, Flash: err.Error()}
	if renderErr := page("error.html").ExecuteTemplate(w, "layout.html", payload); renderErr != nil {
		http.Error(w, renderErr.Error(), http.StatusInternalServerError)
	}
}
