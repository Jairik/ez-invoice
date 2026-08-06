# ez-invoice

`ez-invoice` is a local Go CLI/TUI for tracking billable time, assembling invoices from a date range, and exporting them directly to PDF. Configuration is stored in editable TOML, while presets, time entries, invoice snapshots, and sequential numbers are stored in SQLite.

## Requirements and installation

- Go 1.24 or newer
- A C compiler for the SQLite driver (`gcc` or `clang`)

```sh
# Build the command into the current directory.
go build ./cmd/ez-invoice
```

You can also install it into your Go binary directory:

```sh
# Install ez-invoice using the active Go toolchain.
go install ./cmd/ez-invoice
```

## First run

Running without a subcommand opens the Bubble Tea interface:

```sh
# Start the interactive terminal UI.
./ez-invoice
```

The first run creates `config.toml`, `invoices.db`, and the configured invoice output directory. They live under the platform user-config directory in an `ez-invoice` folder. Use `--data-dir` before the command to choose another location:

```sh
# Keep all local app data in a project-specific directory.
./ez-invoice --data-dir "$PWD/.ez-invoice" help
```

## Configure invoice profiles

The generated config is intentionally incomplete: sender and recipient details must be filled before an invoice can be finalized. Edit `config.toml` directly, run the same commands inside the TUI, or use the CLI helpers:

```sh
# Fill the required sender and default recipient profile.
./ez-invoice config set sender.name "Ada Lovelace"
./ez-invoice config set sender.address "1 Computing Lane"
./ez-invoice config set sender.email "ada@example.com"
./ez-invoice config set recipient.company "Analytical Engines"
./ez-invoice config set recipient.address "2 Difference Road"
./ez-invoice config contact add "Charles Babbage" "charles@example.com"
./ez-invoice config show
```

Supported `config set` keys are `sender.name`, `sender.address`, `sender.email`, `recipient.company`, `recipient.address`, `terms`, `currency`, `logo`, `output`, `notes`, and `adjustment`. Multiple recipients and contacts can be managed with `config recipient add|delete` and `config contact add|delete`. Unknown TOML fields and invalid values are rejected rather than ignored.

A representative configuration looks like this:

```toml
# Central invoice defaults; sender and recipient values are examples.
payable_terms = 'Net 15'
logo_path = ''
currency = 'USD'
output_directory = '/home/user/Invoices'
default_notes = 'None'
default_adjustment = '0.00'

[sender]
full_name = 'Ada Lovelace'
address = '1 Computing Lane'
email = 'ada@example.com'

[[recipients]]
company_name = 'Analytical Engines'
address = '2 Difference Road'

[[contacts]]
name = 'Charles Babbage'
email = 'charles@example.com'
```

## Presets and time entries

Create reusable descriptions and rates, then reference their IDs when adding time:

```sh
# Create presets and add a manual work interval.
./ez-invoice description add "Software development"
./ez-invoice rate add "Standard hourly" 125.00 USD
./ez-invoice time add --start "2026-08-05 09:00" --end "2026-08-05 10:40" --description-preset 1 --rate-preset 1 --notes "API work"
./ez-invoice time list --from 2026-08-01 --to 2026-08-31
```

Datetime values accept RFC3339 or local `YYYY-MM-DD HH:MM`. Hours are derived from the interval and rounded to two decimals. `time edit ID` accepts the same flags and preserves omitted values; `time delete ID` removes an uninvoiced entry. Preset `delete` commands deactivate presets so historical links remain intact, and `restore` reactivates them.

## Preview and generate invoices

Preview shows the table and totals without changing any rows:

```sh
# Preview August entries and exclude row 3 from the selection.
./ez-invoice invoice preview --from 2026-08-01 --to 2026-08-31 --exclude 3
```

Generate snapshots the current profiles and line items, links the selected time entries, allocates a transaction-safe sequence unless a manual number is supplied, and writes the PDF:

```sh
# Finalize with metadata overrides and choose a one-off export directory.
./ez-invoice invoice generate --from 2026-08-01 --to 2026-08-31 --exclude 3 --submitted 2026-09-01 --number "ACME-2026-08" --terms "Net 30" --adjustment -25.00 --notes "Thank you" --contact "Charles Babbage|charles@example.com" --output "$PWD/invoices"
```

Use `invoice list` for history and `invoice export ID --output DIRECTORY` to render a past snapshot again. Later config edits do not change finalized invoices.

## TUI workflow

The TUI opens on a task-oriented dashboard for adding time, reviewing entries, building invoices, browsing invoice history, managing presets, and editing settings. It uses the same validation and persistence behavior as the CLI.

- Use Up and Down to move through menus and rows.
- In the time-entry form, use Left and Right to move between fields, then Up and Down to change dates, times, AM/PM, and presets.
- In other forms, use Up and Down to move and Left and Right to adjust selectable values.
- Use Enter to open an item, edit an exact value, or confirm the primary action.
- Use Escape to cancel an edit or return to the previous screen, and Ctrl+C to quit.

New time entries default both start and end dates to today. Times use separate AM/PM selectors, accept direct 12-hour typing with an optional colon, and move in 15-minute steps with Up and Down. The live total appears below the interval, and a confirmation screen shows the exact dates, times, and duration before saving. End timestamps must be later than start timestamps; separate dates continue to support overnight work. Description and rate fields start with the first active preset; cycle to Custom to enter a one-off value. Invoice creation guides you through dates, exact row selection, metadata, review, and PDF generation.

## Development

```sh
# Format, test, and build the whole application.
go fmt ./...
go test ./...
go build ./cmd/ez-invoice
```

The code is organized into `internal/domain`, `internal/config`, `internal/store/sqlite`, `internal/app`, `internal/cli`, `internal/tui`, and `internal/invoice/pdf`, with the executable under `cmd/ez-invoice`.
