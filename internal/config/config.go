// Package config loads and validates the editable TOML application config.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jairik/ez-invoice/internal/domain"
	"github.com/pelletier/go-toml/v2"
)

// Sender is the invoice issuer profile.
type Sender struct {
	FullName string `toml:"full_name"`
	Address  string `toml:"address"`
	Email    string `toml:"email"`
}

// Recipient is a reusable invoice destination.
type Recipient struct {
	CompanyName string `toml:"company_name"`
	Address     string `toml:"address"`
}

// Contact is a recipient point of contact.
type Contact struct {
	Name  string `toml:"name"`
	Email string `toml:"email"`
}

// Config is the strict, centralized application configuration.
type Config struct {
	Sender            Sender      `toml:"sender"`
	Recipients        []Recipient `toml:"recipients"`
	Contacts          []Contact   `toml:"contacts"`
	PayableTerms      string      `toml:"payable_terms"`
	LogoPath          string      `toml:"logo_path"`
	Currency          string      `toml:"currency"`
	OutputDir         string      `toml:"output_directory"`
	Notes             string      `toml:"default_notes"`
	DefaultAdjustment string      `toml:"default_adjustment"`
}

// Default returns the first-run configuration.
func Default(dataDir string) Config {
	return Config{
		Sender:            Sender{FullName: "Jairik McCauley", Address: "11223 Gehr Rd, Big Pool MD 21711", Email: "mjairik@gmail.com"},
		Recipients:        []Recipient{{CompanyName: "Tenaxiom Technology, Inc", Address: "7459 Digby Grn\nAlexandria, VA 22315"}},
		Contacts:          []Contact{{Name: "Amy Marden", Email: "amy.marden@tenaxiom.tech"}, {Name: "Tito Torres", Email: "tito.torres@tenaxiom.tech"}},
		PayableTerms:      "Net 15",
		LogoPath:          DefaultLogoPath(dataDir),
		Currency:          "USD",
		OutputDir:         filepath.Join(dataDir, "invoices"),
		Notes:             "None",
		DefaultAdjustment: "0.00",
	}
}

// Load decodes a configuration file.
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	var cfg Config
	if err := toml.NewDecoder(file).DisallowUnknownFields().Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config with unknown or invalid field: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes a configuration file.
func Save(path string, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := toml.NewEncoder(temp).Encode(cfg); err != nil {
		temp.Close()
		return fmt.Errorf("encode config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Ensure loads a config or creates its first-run defaults.
func Ensure(path, dataDir string) (Config, bool, error) {
	cfg, err := Load(path)
	if err == nil {
		changed := false
		if cfg.LogoPath == "" {
			cfg.LogoPath = DefaultLogoPath(dataDir)
			changed = true
		}
		if cfg.LogoPath == DefaultLogoPath(dataDir) {
			if err := ensureDefaultLogo(cfg.LogoPath); err != nil {
				return Config{}, false, err
			}
		}
		if changed {
			if err := Save(path, cfg); err != nil {
				return Config{}, false, err
			}
		}
		return cfg, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, err
	}
	cfg = Default(dataDir)
	if err := ensureDefaultLogo(cfg.LogoPath); err != nil {
		return Config{}, false, err
	}
	if err := Save(path, cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

// ValidateForInvoice checks fields required to finalize an invoice.
func (cfg Config) ValidateForInvoice() error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Sender.FullName) == "" || strings.TrimSpace(cfg.Sender.Address) == "" || strings.TrimSpace(cfg.Sender.Email) == "" {
		return fmt.Errorf("sender full name, address, and email are required")
	}
	if !validEmail(cfg.Sender.Email) {
		return fmt.Errorf("sender email is invalid")
	}
	for _, contact := range cfg.Contacts {
		if strings.TrimSpace(contact.Name) == "" || !validEmail(contact.Email) {
			return fmt.Errorf("each contact requires a name and valid email")
		}
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if cfg.LogoPath != "" {
		info, err := os.Stat(cfg.LogoPath)
		if err != nil || info.IsDir() {
			return fmt.Errorf("logo path must reference an existing file")
		}
	}
	return nil
}

// validate checks the editable schema without requiring completed profiles.
func (cfg Config) validate() error {
	if strings.TrimSpace(cfg.PayableTerms) == "" || strings.TrimSpace(cfg.Currency) == "" || strings.TrimSpace(cfg.OutputDir) == "" {
		return fmt.Errorf("payable terms, currency, and output directory are required")
	}
	if len(cfg.Recipients) == 0 {
		return fmt.Errorf("at least one recipient profile is required")
	}
	if _, err := domain.ParseMoney(cfg.DefaultAdjustment); err != nil {
		return fmt.Errorf("default adjustment: %w", err)
	}
	if cfg.Sender.Email != "" && !validEmail(cfg.Sender.Email) {
		return fmt.Errorf("sender email is invalid")
	}
	for _, contact := range cfg.Contacts {
		if contact.Email != "" && !validEmail(contact.Email) {
			return fmt.Errorf("contact email %q is invalid", contact.Email)
		}
	}
	return nil
}

// validEmail applies the basic address validation promised by v1.
func validEmail(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && address.Address == strings.TrimSpace(value) && strings.Contains(address.Address, "@")
}
