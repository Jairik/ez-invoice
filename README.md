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

The generated config is prefilled with the Tenaxiom sender, recipient, contacts, Net 15 terms, and supplied logo. Edit `config.toml` directly, run the same commands inside the TUI, or use the CLI helpers when those defaults need to change:

```sh
# Fill the required sender and default recipient profile.
./ez-invoice config set sender.name "Jairik McCauley"
./ez-invoice config set sender.address "11223 Gehr Rd, Big Pool MD 21711"
./ez-invoice config set sender.email "mjairik@gmail.com"
./ez-invoice config set recipient.company "Tenaxiom Technology, Inc"
./ez-invoice config set recipient.address "7459 Digby Grn\nAlexandria, VA 22315"
./ez-invoice config contact add "Amy Marden" "amy.marden@tenaxiom.tech"
./ez-invoice config show
```

Supported `config set` keys are `sender.name`, `sender.address`, `sender.email`, `recipient.company`, `recipient.address`, `terms`, `currency`, `logo`, `output`, `notes`, and `adjustment`. Multiple recipients and contacts can be managed with `config recipient add|delete` and `config contact add|delete`. Unknown TOML fields and invalid values are rejected rather than ignored.

A representative configuration looks like this:

```toml
# Central invoice defaults; sender and recipient values are examples.
payable_terms = 'Net 15'
logo_path = '/home/user/.config/ez-invoice/tenaxiom-logo.png'
currency = 'USD'
output_directory = '/home/user/Invoices'
default_notes = 'None'
default_adjustment = '0.00'

[sender]
full_name = 'Jairik McCauley'
address = '11223 Gehr Rd, Big Pool MD 21711'
email = 'mjairik@gmail.com'

[[recipients]]
company_name = 'Tenaxiom Technology, Inc'
address = '7459 Digby Grn\nAlexandria, VA 22315'

[[contacts]]
name = 'Amy Marden'
email = 'amy.marden@tenaxiom.tech'

[[contacts]]
name = 'Tito Torres'
email = 'tito.torres@tenaxiom.tech'
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

The TUI opens on a keyboard-first workspace with Overview, Time, Invoices, Presets, and Settings areas. It uses the same validation and persistence behavior as the CLI, but normal use does not require entering commands.

- Use Left and Right to switch workspace areas; Up and Down move through the active rows.
- Overview shows today’s hours, unbilled value, recent work, and direct quick actions.
- Selecting a row reveals Edit, Delete, Restore, or Export actions in the same panel; use Left and Right to choose one, Enter to activate it, and Escape to close the action strip.
- In forms, use Up and Down to move between fields, then Left and Right to change dates, times, AM/PM, and selectable presets.
- Use Enter to start typing a free-form value or activate the highlighted save/continue action. Escape cancels typing or returns to the previous screen; Ctrl+C quits.

New time entries default both start and end dates to today. Times use separate AM/PM selectors, accept direct 12-hour typing with an optional colon, and move in 15-minute steps with Up and Down. The live total appears below the interval, and a confirmation screen shows the exact dates, times, and duration before saving. End timestamps must be later than start timestamps; separate dates continue to support overnight work. Description and rate fields start with the first active preset; cycle to Custom to enter a one-off value. Invoice creation guides you through dates, exact row selection, metadata, review, and PDF generation.

## Development

```sh
# Format, test, and build the whole application.
go fmt ./...
go test ./...
go build ./cmd/ez-invoice
```

The code is organized into `internal/domain`, `internal/config`, `internal/store/sqlite`, `internal/app`, `internal/cli`, `internal/tui`, and `internal/invoice/pdf`, with the executable under `cmd/ez-invoice`.
