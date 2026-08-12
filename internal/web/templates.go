package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
)

// pageView carries the chrome shared by every rendered page.
type pageView struct {
	Title   string
	Active  string
	Flash   string
	FlashOK bool
	Config  config.Config
}

// templatesFS roots the embedded templates directory.
var templatesFS = func() fs.FS {
	root, err := fs.Sub(content, "templates")
	if err != nil {
		panic(err)
	}
	return root
}()

// page returns the parsed layout for one page file.
func page(name string) *template.Template {
	file := name
	if !strings.HasSuffix(file, ".html") {
		file += ".html"
	}
	parsed, _ := pageTemplates()
	return parsed[file]
}

// pageTemplates parses every page once against the shared layout. Parsing
// happens lazily on first use but is synchronized, so concurrent handlers
// never race on the template cache.
var pageTemplates = sync.OnceValues(func() (map[string]*template.Template, error) {
	files, err := fs.Glob(templatesFS, "*.html")
	if err != nil {
		return nil, err
	}
	parsed := make(map[string]*template.Template, len(files))
	for _, file := range files {
		if file == "layout.html" {
			continue
		}
		parsed[file] = template.Must(template.New("root").Funcs(templateFuncs()).
			ParseFS(templatesFS, "layout.html", file))
	}
	return parsed, nil
})

// templateFuncs returns formatting helpers shared by all pages.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"money": domain.FormatMoney,
		"cur": func(currency string, cents int64) string {
			return currency + " " + domain.FormatMoney(cents)
		},
		"date":        func(value time.Time) string { return value.Local().Format("2006-01-02") },
		"dateIn":      func(value time.Time) string { return value.Local().Format("02 Jan") },
		"dateFull":    func(value time.Time) string { return value.Local().Format("02 Jan 2006") },
		"dateLong":    func(value time.Time) string { return value.Local().Format("2 January 2006") },
		"clock":       func(value time.Time) string { return value.Local().Format("15:04") },
		"dateTime":    func(value time.Time) string { return value.Local().Format("2006-01-02 15:04") },
		"dateTimeLoc": func(value time.Time) string { return value.Local().Format("2006-01-02T15:04") },
		"hours":       func(value float64) string { return fmt.Sprintf("%.2f", value) },
		"filesize": func(bytes int64) string {
			switch {
			case bytes >= 1<<20:
				return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
			case bytes >= 1<<10:
				return fmt.Sprintf("%d KB", bytes/(1<<10))
			default:
				return fmt.Sprintf("%d B", bytes)
			}
		},
		"lower": strings.ToLower,
		"dict":  func(values ...any) (map[string]any, error) { return toDict(values) },
		"deref": func(value *int64) int64 {
			if value == nil {
				return 0
			}
			return *value
		},
	}
}

// toDict pairs alternating key and value arguments for template calls.
func toDict(values []any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict requires key/value pairs")
	}
	result := make(map[string]any, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		key, ok := values[index].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		result[key] = values[index+1]
	}
	return result, nil
}

// renderPage executes one cached page template against its layout.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, name string, view pageView, data any) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view.Flash, view.FlashOK = takeFlash(w, r)
	view.Config = s.app.Config()
	payload := struct {
		pageView
		Data any
	}{pageView: view, Data: data}
	if err := page(name).ExecuteTemplate(w, "layout.html", payload); err != nil {
		return fmt.Errorf("render %s: %w", name, err)
	}
	return nil
}
