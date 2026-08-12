package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/domain"
	invoicepdf "github.com/Jairik/ez-invoice/internal/invoice/pdf"
)

// builderView feeds the invoice creation page.
type builderView struct {
	From       string
	To         string
	Entries    []domain.TimeEntry
	Recipient  []recipientOption
	Contacts   []contactOption
	Terms      string
	Notes      string
	Adjustment string
	Currency   string
	Submitted  string
}

type recipientOption struct {
	Index   int
	Company string
	Address string
}

type contactOption struct {
	Index int
	Name  string
	Email string
}

// invoiceBuilder shows the entry selection and metadata form.
func (s *Server) invoiceBuilder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	from, to, err := parseDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), now)
	if err != nil {
		renderError(w, r, s, pageView{Title: "New Invoice", Active: "invoices"}, err)
		return
	}
	entries, err := s.app.Store.ListTimeEntries(ctx, from, to, true)
	if err != nil {
		renderError(w, r, s, pageView{Title: "New Invoice", Active: "invoices"}, err)
		return
	}
	view := builderView{
		From:       from.Format("2006-01-02"),
		To:         to.Add(-time.Second).Format("2006-01-02"),
		Entries:    entries,
		Terms:      s.app.Config().PayableTerms,
		Notes:      s.app.Config().Notes,
		Adjustment: s.app.Config().DefaultAdjustment,
		Currency:   s.app.Config().Currency,
		Submitted:  now.Format("2006-01-02"),
	}
	for index, recipient := range s.app.Config().Recipients {
		view.Recipient = append(view.Recipient, recipientOption{Index: index, Company: recipient.CompanyName, Address: recipient.Address})
	}
	for index, contact := range s.app.Config().Contacts {
		view.Contacts = append(view.Contacts, contactOption{Index: index, Name: contact.Name, Email: contact.Email})
	}
	if err := s.renderPage(w, r, "invoice_builder.html", pageView{Title: "New Invoice", Active: "invoices"}, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// previewRequest is the JSON body of the live preview call.
type previewRequest struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Include    []int64 `json:"include"`
	Adjustment string  `json:"adjustment"`
}

// previewResponse mirrors the selected invoice totals.
type previewResponse struct {
	Count      int    `json:"count"`
	Hours      string `json:"hours"`
	Subtotal   string `json:"subtotal"`
	Adjustment string `json:"adjustment"`
	Total      string `json:"total"`
	Error      string `json:"error,omitempty"`
}

// invoicePreviewJSON returns live totals for the builder selection.
func (s *Server) invoicePreviewJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var request previewRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	from, to, err := parseDateRange(request.From, request.To, now)
	if err != nil {
		writePreviewJSON(w, previewResponse{Error: err.Error()})
		return
	}
	options := app.InvoiceOptions{From: from, To: to, IncludeIDs: request.Include}
	if request.Adjustment != "" {
		adjustment, err := domain.ParseMoney(request.Adjustment)
		if err != nil {
			writePreviewJSON(w, previewResponse{Error: "adjustment: " + err.Error()})
			return
		}
		options.AdjustmentCents = &adjustment
	}
	preview, err := s.app.PreviewInvoice(ctx, options)
	if err != nil {
		writePreviewJSON(w, previewResponse{Error: err.Error()})
		return
	}
	selectedHours := 0.0
	for _, entry := range preview.Entries {
		selectedHours += entry.Hours
	}
	response := previewResponse{
		Count:      len(preview.Entries),
		Hours:      fmt.Sprintf("%.2f", selectedHours),
		Subtotal:   domain.FormatMoney(preview.SubtotalCents),
		Adjustment: domain.FormatMoney(preview.AdjustmentCents),
		Total:      domain.FormatMoney(preview.TotalCents),
	}
	writePreviewJSON(w, response)
}

// writePreviewJSON serializes a preview response.
func writePreviewJSON(w http.ResponseWriter, response previewResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// generateInvoice finalizes the selected entries and renders the PDF.
func (s *Server) generateInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	from, to, err := parseDateRange(r.PostFormValue("from"), r.PostFormValue("to"), now)
	if err != nil {
		redirectTo(w, r, "/invoices/new", false, err.Error())
		return
	}
	include, err := parseIDs(r.PostForm["include"])
	if err != nil {
		redirectTo(w, r, "/invoices/new?from="+from.Format("2006-01-02")+"&to="+to.Format("2006-01-02"), false, err.Error())
		return
	}
	options := app.InvoiceOptions{From: from, To: to, IncludeIDs: include}
	options.SubmittedDate, err = parseOptionalDate(r.PostFormValue("submitted"))
	if err != nil {
		redirectTo(w, r, "/invoices/new", false, err.Error())
		return
	}
	options.NumberOverride = strings.TrimSpace(r.PostFormValue("number"))
	options.PayableTerms = strings.TrimSpace(r.PostFormValue("terms"))
	options.Notes = strings.TrimSpace(r.PostFormValue("notes"))
	if adjustment := strings.TrimSpace(r.PostFormValue("adjustment")); adjustment != "" {
		cents, err := domain.ParseMoney(adjustment)
		if err != nil {
			redirectTo(w, r, "/invoices/new", false, "adjustment: "+err.Error())
			return
		}
		options.AdjustmentCents = &cents
	}
	if recipient := r.PostFormValue("recipient"); recipient != "" {
		options.RecipientIndex, err = strconv.Atoi(recipient)
		if err != nil || options.RecipientIndex < 0 {
			redirectTo(w, r, "/invoices/new", false, "invalid recipient")
			return
		}
	}
	contacts := map[string]bool{}
	for _, index := range r.PostForm["contact"] {
		contacts[index] = true
	}
	if len(contacts) > 0 {
		for index, contact := range s.app.Config().Contacts {
			if contacts[strconv.Itoa(index)] {
				options.Contacts = append(options.Contacts, contact)
			}
		}
	}

	invoice, err := s.app.FinalizeInvoice(ctx, options)
	if err != nil {
		redirectTo(w, r, "/invoices/new?from="+from.Format("2006-01-02")+"&to="+to.Format("2006-01-02"), false, err.Error())
		return
	}
	path := filepath.Join(s.app.Config().OutputDir, invoicepdf.Filename(invoice))
	if err := invoicepdf.Render(invoice, path); err != nil {
		if saveErr := s.app.Store.SetInvoicePDFPath(ctx, invoice.ID, ""); saveErr != nil {
			redirectTo(w, r, "/invoices/"+strconv.FormatInt(invoice.ID, 10), true, "Invoice generated")
			return
		}
		redirectTo(w, r, "/invoices/"+strconv.FormatInt(invoice.ID, 10), true, "Invoice generated but PDF export failed: "+err.Error())
		return
	}
	if err := s.app.Store.SetInvoicePDFPath(ctx, invoice.ID, path); err != nil {
		redirectTo(w, r, "/invoices/"+strconv.FormatInt(invoice.ID, 10), true, "Invoice generated but PDF export failed: "+err.Error())
		return
	}
	redirectTo(w, r, "/invoices/"+strconv.FormatInt(invoice.ID, 10), true, "Invoice "+invoice.DisplayNumber()+" generated")
}

// parseIDs converts repeated form values to positive IDs.
func parseIDs(values []string) ([]int64, error) {
	var ids []int64
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid entry selection")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// parseOptionalDate accepts an empty value as today.
func parseOptionalDate(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Now(), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, errors.New("submitted date must use YYYY-MM-DD")
	}
	return parsed, nil
}

// exportInvoice re-renders a past snapshot to its output directory.
func (s *Server) exportInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	invoice, err := s.app.Store.GetInvoice(ctx, id)
	if err != nil {
		redirectTo(w, r, "/invoices", false, err.Error())
		return
	}
	path := filepath.Join(s.app.Config().OutputDir, invoicepdf.Filename(invoice))
	if err := invoicepdf.Render(invoice, path); err != nil {
		redirectTo(w, r, "/invoices/"+strconv.FormatInt(id, 10), false, err.Error())
		return
	}
	if err := s.app.Store.SetInvoicePDFPath(ctx, id, path); err != nil {
		redirectTo(w, r, "/invoices/"+strconv.FormatInt(id, 10), false, err.Error())
		return
	}
	redirectTo(w, r, "/invoices/"+strconv.FormatInt(id, 10), true, "PDF exported")
}

// downloadPDF serves the latest rendered PDF of an invoice.
func (s *Server) downloadPDF(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	invoice, err := s.app.Store.GetInvoice(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path := invoice.PDFPath
	if path == "" {
		path = filepath.Join(s.app.Config().OutputDir, invoicepdf.Filename(invoice))
	}
	if _, err := os.Stat(path); err != nil {
		if err := invoicepdf.Render(invoice, path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename="+invoicepdf.Filename(invoice))
	http.ServeFile(w, r, path)
}
