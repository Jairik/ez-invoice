# Remediation Report — ez-invoice

- Review run: `docs/temp/agent-security-reviews/2026-08-12T10-07-46/`
- Model: DeepSeekV4Flash
- Source: `DeepSeekV4Flash-Review.md` in the same run directory
- Approval: blanket approval granted by the user ("If you find anything, please remediate it")

## Validation summary

Every finding from the review was independently re-validated against the current code
before and after changes (trace data flow, callers, guards, tests). Classifications:

| Source key | Finding | Verdict | Outcome |
|------------|---------|---------|---------|
| D4F1 / F-015 | Hours rounding | Rejected (intended design, documented + tested) | No change |
| W1F1 / F-001 | CSRF | Confirmed | Fixed |
| W1F2 / F-002 | Settings paths | Confirmed, mitigated by F-001 fix (owner-only trust) | No further change |
| W1F3 / F-003 | Unbounded JSON body | Confirmed | Fixed |
| W1F4 / F-004 | Overflow in preview/dashboard sums | Confirmed | Fixed |
| D3F2 / F-008 | LineTotal 2^63 boundary | Confirmed | Fixed |
| D3F3 / F-009 | app.Config data race | Confirmed | Fixed |
| T2F1 / F-005 | Overview includes invoiced | Confirmed | Fixed |
| T2F2 / F-006 | Midnight rollover | Confirmed | Fixed |
| T2F3 / F-007 | Config mutated before save | Confirmed | Fixed |
| C4F1 / F-010 | PDF row across pages | Confirmed | Fixed |
| C4F2 / F-011 | PDF non-ASCII mojibake | Confirmed | Fixed |
| C4F3 / F-012 | CLI trailing args ignored | Confirmed | Fixed |
| C4F4 / F-013 | Bare port rejected | Confirmed | Fixed |
| C4F5 / F-014 | serve.sh host:port broken | Confirmed | Fixed |

## Changes applied

### internal/domain/calculations.go
- `LineTotal` overflow guard changed from `total > math.MaxInt64` to `total >= 1<<63`
  (F-008). `float64(math.MaxInt64)` rounds up to 2^63, so the old comparison allowed
  the exact boundary through and `int64(2^63)` wrapped to MinInt64, producing negative
  line totals that bypassed the store's `lineTotal > 0` guard.

### internal/app/app.go
- `App.Config` replaced by mutex-protected accessors `Config()` / `SetConfig()` with a
  private field (F-009). `Open` populates via `SetConfig`.

### internal/app/invoice.go
- `app.Config = cfg` → `app.SetConfig(cfg)` (F-009).
- Preview subtotal and total accumulation now carry the same overflow guards as the
  persisted path (F-004): `subtotal > MaxInt64-line` and `subtotal > MaxInt64-adjustment`.

### internal/web/server.go
- New `enforceSameOrigin` middleware wraps every route (F-001). State-changing requests
  (non-GET/HEAD/OPTIONS) are rejected with 403 when the `Origin` header (or `Referer`
  when Origin is absent) does not match the request `Host`. Browser cross-origin POSTs
  always carry an Origin header; curl/scripts without either are accepted (server is
  localhost-bound, no session cookies).

### internal/web/invoice.go
- `r.Body` wrapped with `http.MaxBytesReader(w, r.Body, 1<<20)` in `invoicePreviewJSON`
  (F-003).
- All `s.app.Config.X` reads → `s.app.Config().X` (F-009).

### internal/web/actions.go, templates.go, handlers.go
- Config accessor conversion (F-009).
- Dashboard `MonthValue`/`Unbilled`/`Invoiced` sums use a new `saturatingAdd` helper
  that clamps at MaxInt64 instead of wrapping (F-004).

### internal/tui/model.go
- `loadOverview` now requests `uninvoicedOnly=true` (F-005), matching the invoice
  builder and the web dashboard; the "UNBILLED" total and recent-work list no longer
  count finalized entries.

### internal/tui/forms.go
- `adjustTimeField` now detects midnight crossings (day change in the parsed clock)
  and moves the paired date field (Start/End date) by ±1 day via new `adjustDateField`
  (F-006). Noon rollover (11:45 AM → 12:00 PM) does not change the date.

### internal/tui/settings.go
- `saveRecipient`, `saveContact`, and `updateProfileDeleteConfirmation` deep-copy the
  `Recipients`/`Contacts` slices before mutating, so a failed `config.Save` no longer
  leaves the in-memory config divergent from disk (F-007).
- `saveConfig` uses `SetConfig` (F-009).

### internal/cli/cli.go
- New `rejectExtraArgs` helper called after every `flag.Parse` in `invoice export`,
  `parseInvoiceFlags`, `time list`, and `parseTimeEntryFlags`; leftover positional
  arguments now fail with `unexpected argument %q` (F-012).
- Config accessor conversion (F-009).

### cmd/ez-invoice/main.go
- `normalizeAddr` maps a bare port to `127.0.0.1:<port>` (F-013), matching the README.

### serve.sh
- When the first argument already contains `:`, it is passed through unchanged and the
  URL is built from it directly; otherwise `127.0.0.1:` is prepended as before (F-014).

### internal/invoice/pdf/pdf.go
- All user text (sender/recipient blocks, contacts, number, terms, notes, line
  descriptions, currency amounts) is translated to cp1252 via
  `document.UnicodeTranslatorFromDescriptor("")` before `CellFormat`/`MultiCell`
  (F-011). Core fonts declare WinAnsi encoding; this eliminates raw UTF-8 mojibake.
  Unmappable code points (e.g. emoji) degrade to a single dot instead of corrupting
  surrounding text.
- `renderLineItems` now splits rows taller than the usable page height into per-page
  chunks (max lines per page from page geometry), repeating the table header per page
  and drawing the price/units/total cells only on the final chunk of the row (F-010).

## New regression tests

- `internal/domain/calculations_test.go`: `LineTotal(MaxInt64, 1)` must error.
- `internal/web/web_test.go`: cross-origin POST rejected (403); same-origin POST with
  matching Origin allowed.
- `internal/cli/cli_test.go`: trailing positional args rejected across `time list`,
  `time add`, `invoice preview`, `invoice export`.
- `internal/tui/model_test.go`: midnight forward/backward adjustments move the paired
  date field; noon rollover leaves the date alone; failed config save leaves memory
  config unchanged; overview excludes invoiced entries.
- `internal/invoice/pdf/pdf_test.go`: raw UTF-8 bytes absent from rendered PDF;
  oversized description spans multiple pages.
- `cmd/ez-invoice/main_test.go`: `normalizeAddr` bare-port / host:port handling.

## Verification performed

- `go build ./...` — pass
- `go vet ./...` — pass
- `go test ./...` — all packages pass (including new tests)
- `go test -race ./...` — all packages pass; the app.Config race (F-009) is exercised
  by the web handler tests and no longer reported
- `bash -n serve.sh` — pass

Not run: GUI browser session against the live server; PDF visual rendering to images.
Recommended follow-up: a short manual browser pass over the settings and invoice
flows, and a visual check of one exported PDF.

## Files changed

- cmd/ez-invoice/main.go, cmd/ez-invoice/main_test.go, serve.sh
- internal/app/app.go, internal/app/app_test.go, internal/app/invoice.go,
  internal/app/invoice_test.go
- internal/cli/cli.go, internal/cli/cli_test.go
- internal/domain/calculations.go, internal/domain/calculations_test.go
- internal/invoice/pdf/pdf.go, internal/invoice/pdf/pdf_test.go
- internal/tui/forms.go, internal/tui/model.go, internal/tui/model_test.go,
  internal/tui/settings.go
- internal/web/actions.go, internal/web/handlers.go, internal/web/invoice.go,
  internal/web/server.go, internal/web/templates.go, internal/web/web_test.go
