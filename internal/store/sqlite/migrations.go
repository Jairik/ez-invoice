package sqlite

import (
	"context"
	"fmt"
	"time"
)

// migration is an ordered schema change.
type migration struct {
	version int
	sql     string
}

var migrations = []migration{{version: 1, sql: `
CREATE TABLE rate_presets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    label TEXT NOT NULL UNIQUE,
    amount INTEGER NOT NULL CHECK (amount >= 0),
    currency TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE description_presets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    label TEXT NOT NULL UNIQUE,
    active INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE invoices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_number_seq INTEGER UNIQUE,
    invoice_number_override TEXT NOT NULL DEFAULT '',
    submitted_date TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    from_name TEXT NOT NULL,
    from_address TEXT NOT NULL,
    from_email TEXT NOT NULL,
    to_company TEXT NOT NULL,
    to_address TEXT NOT NULL,
    payable_terms TEXT NOT NULL,
    currency TEXT NOT NULL,
    notes TEXT NOT NULL,
    adjustment_amount INTEGER NOT NULL DEFAULT 0,
    subtotal_amount INTEGER NOT NULL,
    total_amount INTEGER NOT NULL,
    pdf_path TEXT NOT NULL DEFAULT '',
    logo_path TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX invoice_number_override_unique
    ON invoices(invoice_number_override) WHERE invoice_number_override <> '';
CREATE TABLE time_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    start_at TEXT NOT NULL,
    end_at TEXT NOT NULL,
    hours REAL NOT NULL CHECK (hours >= 0),
    description TEXT NOT NULL,
    rate_amount INTEGER NOT NULL CHECK (rate_amount >= 0),
    currency TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    description_preset_id INTEGER REFERENCES description_presets(id),
    rate_preset_id INTEGER REFERENCES rate_presets(id),
    invoice_id INTEGER REFERENCES invoices(id)
);
CREATE INDEX time_entries_start_at ON time_entries(start_at);
CREATE INDEX time_entries_invoice_id ON time_entries(invoice_id);
CREATE TABLE invoice_contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_id INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    email TEXT NOT NULL
);
CREATE TABLE invoice_line_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_id INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    source_time_entry_id INTEGER REFERENCES time_entries(id),
    description TEXT NOT NULL,
    unit_price INTEGER NOT NULL CHECK (unit_price >= 0),
    units REAL NOT NULL CHECK (units >= 0),
    line_total INTEGER NOT NULL
);
CREATE TABLE invoice_sequence (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    next_value INTEGER NOT NULL CHECK (next_value > 0)
);
INSERT INTO invoice_sequence(singleton, next_value) VALUES (1, 1);
`}}

// migrate applies unapplied schema versions transactionally.
func (store *Store) migrate(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	for _, migration := range migrations {
		var applied int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied != 0 {
			continue
		}
		tx, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, migration.version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}
	return nil
}
