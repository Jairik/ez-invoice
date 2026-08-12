# Security & Quality Review — ez-invoice

- Review time: 2026-08-12T10:07-07:00 (local)
- Model: DeepSeekV4Flash
- Scope: entire repository (working tree incl. untracked TUI/web files), reviewed via four parallel review subagents with independent validation against primary source
- Tests: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` all pass after remediation

## Counts by severity

| Severity | Count | Confirmed | Rejected |
|----------|-------|-----------|----------|
| Critical | 0     | 0         | 0        |
| High     | 2     | 1         | 1        |
| Medium   | 9     | 8         | 1        |
| Low      | 5     | 5         | 0        |

## Findings

### F-001 [High] [High confidence] — CSRF: no origin validation on any state-changing route
- Category: Security (CSRF)
- Location: `internal/web/server.go:26-50` (all POST routes); verified in `actions.go`, `invoice.go`
- Evidence: every route from `POST /time/create` to `POST /settings` is registered without a CSRF token, Origin/Referer check, or auth requirement. The only cookie (flash) is SameSite=Lax but is not used for authorization.
- Trigger: while the app serves on 127.0.0.1:9090, a victim visits a malicious page that POSTs a simple form (no preflight, no cookies needed) to `/time/create`, `/settings`, `/invoices/generate`, `/time/{id}/delete`, `/presets/rates/{id}/toggle`, etc.
- Impact: a remote cross-site page can create/delete time entries, generate invoices with attacker-chosen amounts/recipients/notes, deactivate presets, and rewrite the config — financial record integrity compromise.
- Why existing guards do not resolve it: no auth is expected (local single-user), but no request is bound to any origin; SameSite=Lax on the flash cookie protects nothing since no request requires a cookie.
- Fix direction: reject state-changing requests whose Origin/Referer host does not match the request Host (browsers always send Origin on cross-origin POSTs).

### F-002 [Medium] [Medium confidence] — Unconstrained filesystem access via settings paths
- Category: Security (path handling)
- Location: `internal/web/actions.go:411-412`, `internal/web/invoice.go:194`, `internal/invoice/pdf/pdf.go:62`
- Evidence: `cfg.LogoPath`/`cfg.OutputDir` come verbatim from POST form values; `document.Image(invoice.LogoPath, ...)` reads any local file; `filepath.Join(OutputDir, ...)` writes PDFs to any directory.
- Trigger: POST /settings with `logo`/`output` set to arbitrary paths, then generate an invoice.
- Impact: arbitrary local image read embedded into a PDF; constrained-name file writes in attacker-chosen directories.
- Why existing guards do not resolve it: `config.validate()` only requires non-empty values; `ValidateForInvoice` only stats the logo. Filename sanitization covers the name, not the directory.
- Fix direction: **Mitigated by F-001 remediation.** After the CSRF fix, only the app owner (who can already write anywhere on their own machine) can set these paths — the standard trust model for a local single-user app. Confining paths to the data dir would break legitimate usage. No code change required; fpdf also rejects non-image inputs at render time.

### F-003 [Low] [High confidence] — Unbounded request body on /invoices/preview
- Category: DoS
- Location: `internal/web/invoice.go:100`
- Evidence: `json.NewDecoder(r.Body).Decode(&request)` without `http.MaxBytesReader` or Content-Length cap.
- Fix direction: wrap body with `http.MaxBytesReader(w, r.Body, 1<<20)`. **Remediated.**

### F-004 [Low] [Medium confidence] — Integer overflow in preview/dashboard totals
- Category: Correctness (overflow)
- Location: `internal/app/invoice.go:107,121`; `internal/web/handlers.go:75-79`
- Evidence: `preview.SubtotalCents += entry.LineTotalCents()` and `TotalCents = Subtotal + Adjustment` lack the guard that exists in `store.go:296-309`; dashboard sums likewise.
- Fix direction: mirror the store's `sum > MaxInt64-line` checks. **Remediated.**

### F-005 [Medium] [High confidence] — Overview "UNBILLED" total includes invoiced entries
- Category: Correctness (data mislabel)
- Location: `internal/tui/model.go:568`
- Evidence: `loadOverview` passes `uninvoicedOnly=false` while the invoice flow (`tui/invoice.go:41`) uses `true`; `homeView` renders the sum as "UNBILLED".
- Fix direction: pass `true`. **Remediated.**

### F-006 [Medium] [High confidence] — 15-minute adjustment rewrites a time to midnight on the wrong date
- Category: Correctness (time math)
- Location: `internal/tui/forms.go:193-211`
- Evidence: `adjustTimeField` adds 15 minutes to a 12-hour clock but never touches the paired date field; 11:45 PM + Right becomes 12:00 AM on the same date (23h45m earlier) and the interval validation passes.
- Fix direction: when the clock crosses midnight, advance/regress the paired date field. **Remediated.**

### F-007 [Low] [Medium confidence] — Config mutated in memory before save succeeds
- Category: Data integrity
- Location: `internal/tui/settings.go:113-124,157-168,191-211`
- Evidence: `cfg := model.application.Config` is a shallow copy; `cfg.Recipients[i] = recipient` and delete-path `append(slice[:i], slice[i+1:]...)` write through the shared backing array into `app.Config` before `config.Save` succeeds.
- Impact: on a failed save the on-disk config is unchanged but the running app already reflects the edit; restart silently reverts.
- Fix direction: deep-copy the slices before mutation. **Remediated.**

### F-008 [Medium] [High confidence] — LineTotal overflow guard misses the exact 2^63 boundary
- Category: Correctness (overflow)
- Location: `internal/domain/calculations.go:27-30`
- Evidence: `float64(math.MaxInt64)` rounds up to 2^63, so `total > math.MaxInt64` is false when `total == 2^63`; `int64(math.Round(total))` then wraps to MinInt64 and the negative value flows into SubtotalCents (the store's `lineTotal > 0` guard is bypassed).
- Fix direction: reject `total >= 1<<63`. **Remediated.**

### F-009 [Low] [High confidence] — Data race on shared app.Config
- Category: Concurrency
- Location: `internal/app/invoice.go:64` (write), read at `internal/web/invoice.go:62-68,182,194`, `actions.go:332,359`, `templates.go:94`
- Evidence: `assembleInvoice` performs `app.Config = cfg` inside HTTP request goroutines while other handlers read `s.app.Config` concurrently; no synchronization exists.
- Fix direction: guard Config with `sync.RWMutex` accessors. **Remediated** (all read/write sites converted; verified with `go test -race`).

### F-010 [Medium] [High confidence] — PDF row broken across pages for overlong descriptions
- Category: Correctness (PDF layout)
- Location: `internal/invoice/pdf/pdf.go:136-155`
- Evidence: for a description taller than the usable page height, the pre-break check only moves to a new page; `MultiCell` then auto-breaks mid-row while the price/units/total cells are drawn at the stale `y`, producing giant borders and amounts scattered on separate pages.
- Fix direction: chunk rows taller than a page into per-page segments, drawing side cells on the final segment. **Remediated.**

### F-011 [Medium] [High confidence] — Non-ASCII text renders as mojibake in PDFs
- Category: Correctness (encoding)
- Location: `internal/invoice/pdf/pdf.go` (all `CellFormat`/`MultiCell` text paths)
- Evidence: core fonts use WinAnsi encoding but raw UTF-8 bytes are written; verified "Café — ☕" renders as "CafÃ© â€" â˜•".
- Fix direction: translate text to cp1252 via `UnicodeTranslatorFromDescriptor("")` before rendering. **Remediated.**

### F-012 [Medium] [High confidence] — CLI trailing positional arguments silently ignored
- Category: Correctness (CLI)
- Location: `internal/cli/cli.go:118-120,156-158,524-526,579-581`
- Evidence: `flags.Parse` never rejects `flags.Args()`; verified `time add ... stray` creates the entry and `invoice generate ... junk` finalizes the invoice.
- Impact: silent loss of user intent (e.g. a forgotten `--exclude` argument overbills).
- Fix direction: reject non-empty `flags.Args()`. **Remediated.**

### F-013 [Low] [High confidence] — README bare-port form not accepted by `serve`
- Category: Docs/behavior mismatch
- Location: `cmd/ez-invoice/main.go:36-39`; README.md:136-137
- Evidence: `./ez-invoice serve 8080` fails with "missing port in address" although the README advertises "port, or host and port".
- Fix direction: normalize a bare port to `127.0.0.1:<port>`. **Remediated.**

### F-014 [Low] [High confidence] — serve.sh host:port argument produces a broken URL and listen address
- Category: Correctness (script)
- Location: `serve.sh:7-8,20`
- Evidence: `PORT="0.0.0.0:8080"` yields `http://127.0.0.1:0.0.0.0:8080` and a failed listen.
- Fix direction: pass host:port through unchanged. **Remediated.**

### F-015 [High] [High confidence] — REJECTED: hours rounded to 2 decimals before money multiplication
- Location: `internal/domain/calculations.go:14-19,22-31`
- Reviewer claim: 1h40m at $125.50/hr bills $209.59 instead of $209.17 (exact duration).
- Independent verdict: **Intended, documented design.** README.md:96 explicitly documents "Hours are derived from the interval and rounded to two decimals"; `TestHours`/`TestLineTotal` (calculations_test.go:16-18,31-32) encode the centihour billing granularity; the invoice PDF, preview, and dashboard all use the same rounded `Units`, so the invoice is internally consistent (`1.67 × $125.50 = $209.59`). Changing it would change billed amounts (a product decision) and requires test/README changes. Rejected; documented here rather than silently altering billing behavior.

## Files inspected

- cmd/ez-invoice/main.go, main_test.go; serve.sh; README.md
- internal/{app,cli,config,domain,invoice/pdf,store/sqlite,tui,web}/*.go (all)
- internal/web/templates/*.html, static/app.js, static/style.css
- fpdf@v0.9.0 module sources (SplitLines, MultiCell, UnicodeTranslator, Image) and bubbletea@v1.3.10 (signal handling)

## Material limitations

- Static analysis + live CLI/PDF probes; no GUI browser session was run against the web UI (origin-check middleware validated via httptest).
- PDF visual layout verified by byte-level page/text assertions, not by rendering to an image.
