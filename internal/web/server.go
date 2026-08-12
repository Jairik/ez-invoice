// Package web serves the browser interface for ez-invoice.
package web

import (
	"embed"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Jairik/ez-invoice/internal/app"
)

//go:embed templates static
var content embed.FS

// Server owns the HTTP routes and the open application.
type Server struct {
	app *app.App
}

// New builds a server around an open application.
func New(application *app.App) *Server {
	return &Server{app: application}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", staticAssets(http.FileServer(http.FS(content))))
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("GET /time", s.timePage)
	mux.HandleFunc("POST /time/create", s.createTime)
	mux.HandleFunc("POST /time/{id}/update", s.updateTime)
	mux.HandleFunc("POST /time/{id}/delete", s.deleteTime)
	mux.HandleFunc("GET /presets", s.presetsPage)
	mux.HandleFunc("POST /presets/rates/create", s.createRate)
	mux.HandleFunc("POST /presets/rates/{id}/update", s.updateRate)
	mux.HandleFunc("POST /presets/rates/{id}/toggle", s.toggleRate)
	mux.HandleFunc("POST /presets/descriptions/create", s.createDescription)
	mux.HandleFunc("POST /presets/descriptions/{id}/update", s.updateDescription)
	mux.HandleFunc("POST /presets/descriptions/{id}/toggle", s.toggleDescription)
	mux.HandleFunc("GET /invoices", s.invoicesPage)
	mux.HandleFunc("GET /invoices/new", s.invoiceBuilder)
	mux.HandleFunc("POST /invoices/preview", s.invoicePreviewJSON)
	mux.HandleFunc("POST /invoices/generate", s.generateInvoice)
	mux.HandleFunc("GET /invoices/{id}", s.invoiceDetail)
	mux.HandleFunc("POST /invoices/{id}/export", s.exportInvoice)
	mux.HandleFunc("GET /invoices/{id}/pdf", s.downloadPDF)
	mux.HandleFunc("GET /settings", s.settingsPage)
	mux.HandleFunc("POST /settings", s.saveSettings)
	return enforceSameOrigin(mux)
}

// staticAssets lets browsers cache embedded assets briefly, then revalidate
// cheaply with If-Modified-Since (FileServer still emits Last-Modified).
func staticAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		next.ServeHTTP(w, r)
	})
}

// enforceSameOrigin blocks cross-site state-changing requests sent by a
// browser page from another origin. Requests without an Origin or Referer
// header (curl, scripts, tests) are accepted because the server only binds
// to localhost and has no browser-session cookies.
func enforceSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		if origin != "" && !originHostMatches(origin, r.Host) {
			http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originHostMatches reports whether the parsed origin or referer host matches
// the request Host header, ignoring scheme and path.
func originHostMatches(origin, host string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, host)
}

// Serve runs the web interface until the process stops.
func Serve(application *app.App, addr string) error {
	server := New(application)
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		return fmt.Errorf("serve web interface on %s: %w", addr, err)
	}
	return nil
}
