package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// testServer opens an isolated app and its web handler.
func testServer(t *testing.T) *Server {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "ez-invoice")
	application, err := app.Open(dataDir)
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { application.Close() })
	return New(application)
}

// request performs one HTTP round trip against the server.
func request(t *testing.T, server *Server, method, path string, body url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		reader = strings.NewReader(body.Encode())
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	return recorder
}

// seedEntry creates one time entry in a past month for invoice tests.
func seedEntry(t *testing.T, server *Server, monthOffset int, day, hour int) int64 {
	t.Helper()
	start := time.Date(2026, time.Month(monthOffset), day, hour, 0, 0, 0, time.Local)
	entry, err := server.app.Store.CreateTimeEntry(t.Context(), domain.TimeEntry{
		StartAt: start, EndAt: start.Add(time.Hour), Description: "API work",
		RateAmountCents: 12500, Currency: "USD",
	})
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	return entry.ID
}

func TestDashboardRenders(t *testing.T) {
	server := testServer(t)
	recorder := request(t, server, "GET", "/", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Overview") {
		t.Fatalf("dashboard missing overview content")
	}
}

func TestTimePagesRenderAndCreate(t *testing.T) {
	server := testServer(t)
	for _, path := range []string{"/time", "/presets", "/invoices", "/invoices/new", "/settings"} {
		recorder := request(t, server, "GET", path, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, recorder.Code)
		}
	}

	form := url.Values{
		"start":       {"2026-08-05T09:00"},
		"end":         {"2026-08-05T10:40"},
		"description": {"API work"},
		"rate":        {"125.00"},
		"currency":    {"USD"},
		"notes":       {""},
	}
	recorder := request(t, server, "POST", "/time/create", form)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("create time = %d, want 303", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/time" {
		t.Fatalf("redirect to %q, want /time", location)
	}

	entries, err := server.app.Store.ListTimeEntries(t.Context(),
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local),
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local), true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %v, err = %v", entries, err)
	}
	if entries[0].Hours != 1.67 || entries[0].LineTotalCents() != 20875 {
		t.Fatalf("entry = %+v", entries[0])
	}
}

func TestPreviewJSON(t *testing.T) {
	server := testServer(t)
	seedEntry(t, server, 8, 5, 9)
	body := `{"from":"2026-08-01","to":"2026-08-31","include":[1],"adjustment":"-25.00"}`
	req := httptest.NewRequest("POST", "/invoices/preview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200", recorder.Code)
	}
	var preview previewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.Count != 1 || preview.Subtotal != "125.00" || preview.Adjustment != "-25.00" || preview.Total != "100.00" {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestGenerateInvoiceRendersPDF(t *testing.T) {
	server := testServer(t)
	id := seedEntry(t, server, 8, 5, 9)
	form := url.Values{
		"from":      {"2026-08-01"},
		"to":        {"2026-08-31"},
		"include":   {strconv.FormatInt(id, 10)},
		"recipient": {"0"},
		"submitted": {"2026-09-01"},
	}
	recorder := request(t, server, "POST", "/invoices/generate", form)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("generate = %d, want 303", recorder.Code)
	}
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "/invoices/") {
		t.Fatalf("redirect to %q", location)
	}
	invoices, err := server.app.Store.ListInvoices(t.Context())
	if err != nil || len(invoices) != 1 {
		t.Fatalf("invoices = %v, err = %v", invoices, err)
	}
	if invoices[0].TotalCents != 12500 {
		t.Fatalf("invoice total = %d, want 12500", invoices[0].TotalCents)
	}
	if invoices[0].PDFPath == "" {
		t.Fatalf("invoice PDF path not recorded")
	}

	recorder = request(t, server, "GET", location+"/pdf", nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "pdf") {
		t.Fatalf("pdf = %d, type = %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestPresetsCRUD(t *testing.T) {
	server := testServer(t)
	form := url.Values{"label": {"Standard hourly"}, "amount": {"125.00"}}
	recorder := request(t, server, "POST", "/presets/rates/create", form)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("create rate = %d, want 303", recorder.Code)
	}
	rates, err := server.app.Store.ListRatePresets(t.Context(), true)
	if err != nil || len(rates) != 1 {
		t.Fatalf("rates = %v, err = %v", rates, err)
	}

	recorder = request(t, server, "POST", "/presets/rates/1/toggle", url.Values{"active": {"0"}})
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("toggle rate = %d, want 303", recorder.Code)
	}
	rates, _ = server.app.Store.ListRatePresets(t.Context(), true)
	if len(rates) != 1 || rates[0].Active {
		t.Fatalf("rate still active: %+v", rates)
	}
}

func TestSettingsSaveAndReload(t *testing.T) {
	server := testServer(t)
	form := url.Values{
		"sender_name":       {"Jane Doe"},
		"sender_email":      {"jane@example.com"},
		"sender_address":    {"1 Main St"},
		"recipient_company": {"Acme Inc"},
		"recipient_address": {"2 Elm St"},
		"terms":             {"Net 30"},
		"currency":          {"EUR"},
		"output":            {server.app.Config().OutputDir},
		"adjustment":        {"0.00"},
		"notes":             {"Thanks"},
	}
	recorder := request(t, server, "POST", "/settings", form)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("save settings = %d, want 303", recorder.Code)
	}
	if server.app.Config().Sender.FullName != "Jane Doe" || server.app.Config().Currency != "EUR" {
		t.Fatalf("config not updated: %+v", server.app.Config())
	}
}

func TestSettingsRejectsInvalid(t *testing.T) {
	server := testServer(t)
	form := url.Values{
		"recipient_company": {"Acme Inc"},
		"recipient_address": {"2 Elm St"},
		"currency":          {"USD"},
		"output":            {server.app.Config().OutputDir},
		"adjustment":        {"not-a-number"},
	}
	recorder := request(t, server, "POST", "/settings", form)
	if recorder.Code != http.StatusOK {
		t.Fatalf("invalid settings = %d, want 200 with error", recorder.Code)
	}
}

func TestCrossOriginPostIsRejected(t *testing.T) {
	server := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/time/create", strings.NewReader("description=csrf"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.example")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST = %d, want 403", recorder.Code)
	}
}

func TestSameOriginPostIsAllowed(t *testing.T) {
	server := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/time/create", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "127.0.0.1:9090"
	req.Header.Set("Origin", "http://127.0.0.1:9090")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("same-origin POST was rejected as cross-origin")
	}
}
