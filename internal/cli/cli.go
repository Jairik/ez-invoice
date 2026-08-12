// Package cli implements non-interactive helpers shared by the terminal UI.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Jairik/ez-invoice/internal/app"
	"github.com/Jairik/ez-invoice/internal/config"
	"github.com/Jairik/ez-invoice/internal/domain"
	invoicepdf "github.com/Jairik/ez-invoice/internal/invoice/pdf"
)

// FinalizedInvoiceError reports that persistence succeeded but PDF export did not.
type FinalizedInvoiceError struct {
	InvoiceID     int64
	InvoiceNumber string
	Err           error
}

// Error explains the partial success without suggesting finalization can be retried.
func (err *FinalizedInvoiceError) Error() string {
	return fmt.Sprintf("invoice %s finalized but PDF export failed: %v", err.InvoiceNumber, err.Err)
}

// Unwrap exposes the underlying export error.
func (err *FinalizedInvoiceError) Unwrap() error { return err.Err }

// Run executes one command against an open application.
func Run(ctx context.Context, application *app.App, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" {
		printHelp(stdout)
		return nil
	}
	switch args[0] {
	case "config":
		return runConfig(application, args[1:], stdout)
	case "rate":
		return runRate(ctx, application, args[1:], stdout)
	case "description":
		return runDescription(ctx, application, args[1:], stdout)
	case "time":
		return runTime(ctx, application, args[1:], stdout, stderr)
	case "invoice":
		return runInvoice(ctx, application, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q; run help", args[0])
	}
}

// runInvoice previews, finalizes, lists, and re-exports invoices.
func runInvoice(ctx context.Context, application *app.App, args []string, output, errorsOutput io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: invoice preview|generate|list|export")
	}
	switch args[0] {
	case "preview", "generate":
		options, outputDir, err := parseInvoiceFlags(args[1:], errorsOutput)
		if err != nil {
			return err
		}
		if args[0] == "preview" {
			preview, err := application.PreviewInvoice(ctx, options)
			if err != nil {
				return err
			}
			printInvoicePreview(output, application.Config().Currency, preview)
			return nil
		}
		invoice, err := application.FinalizeInvoice(ctx, options)
		if err != nil {
			return err
		}
		if outputDir == "" {
			outputDir = application.Config().OutputDir
		}
		path := filepath.Join(outputDir, invoicepdf.Filename(invoice))
		if err := invoicepdf.Render(invoice, path); err != nil {
			return &FinalizedInvoiceError{InvoiceID: invoice.ID, InvoiceNumber: invoice.DisplayNumber(), Err: err}
		}
		if err := application.Store.SetInvoicePDFPath(ctx, invoice.ID, path); err != nil {
			return &FinalizedInvoiceError{InvoiceID: invoice.ID, InvoiceNumber: invoice.DisplayNumber(), Err: err}
		}
		fmt.Fprintf(output, "generated invoice %s: %s\n", invoice.DisplayNumber(), path)
		return nil
	case "list":
		invoices, err := application.Store.ListInvoices(ctx)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(output, 0, 2, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tNUMBER\tSUBMITTED\tPERIOD\tTOTAL\tCURRENCY\tPDF")
		for _, invoice := range invoices {
			fmt.Fprintf(writer, "%d\t%s\t%s\t%s to %s\t%s\t%s\t%s\n", invoice.ID, invoice.DisplayNumber(),
				invoice.SubmittedDate.Format("2006-01-02"), invoice.PeriodStart.Format("2006-01-02"), invoice.PeriodEnd.Format("2006-01-02"),
				domain.FormatMoney(invoice.TotalCents), invoice.Currency, invoice.PDFPath)
		}
		return writer.Flush()
	case "export":
		if len(args) < 2 {
			return errors.New("usage: invoice export ID [--output DIRECTORY]")
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
	flags := flag.NewFlagSet("invoice export", flag.ContinueOnError)
	flags.SetOutput(errorsOutput)
	outputDir := flags.String("output", application.Config().OutputDir, "export directory")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if err := rejectExtraArgs(flags); err != nil {
		return err
	}
		invoice, err := application.Store.GetInvoice(ctx, id)
		if err != nil {
			return err
		}
		path := filepath.Join(*outputDir, invoicepdf.Filename(invoice))
		if err := invoicepdf.Render(invoice, path); err != nil {
			return err
		}
		if err := application.Store.SetInvoicePDFPath(ctx, id, path); err != nil {
			return err
		}
		fmt.Fprintf(output, "exported invoice %s: %s\n", invoice.DisplayNumber(), path)
		return nil
	default:
		return errors.New("usage: invoice preview|generate|list|export")
	}
}

// parseInvoiceFlags converts CLI metadata and row-selection flags.
func parseInvoiceFlags(args []string, errorsOutput io.Writer) (app.InvoiceOptions, string, error) {
	flags := flag.NewFlagSet("invoice", flag.ContinueOnError)
	flags.SetOutput(errorsOutput)
	fromText := flags.String("from", "", "first date")
	toText := flags.String("to", "", "last date")
	includeText := flags.String("include", "", "comma-separated entry IDs")
	excludeText := flags.String("exclude", "", "comma-separated entry IDs")
	submittedText := flags.String("submitted", time.Now().Format("2006-01-02"), "submitted date")
	recipient := flags.Int("recipient", 1, "recipient profile number")
	number := flags.String("number", "", "manual invoice number")
	terms := flags.String("terms", "", "payable terms override")
	notes := flags.String("notes", "", "notes override")
	adjustmentText := flags.String("adjustment", "", "adjustment override")
	outputDir := flags.String("output", "", "export directory")
	var contacts stringList
	flags.Var(&contacts, "contact", "contact override as Name|email (repeatable)")
	if err := flags.Parse(args); err != nil {
		return app.InvoiceOptions{}, "", err
	}
	if err := rejectExtraArgs(flags); err != nil {
		return app.InvoiceOptions{}, "", err
	}
	includeProvided := false
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "include" {
			includeProvided = true
		}
	})
	if *fromText == "" || *toText == "" {
		return app.InvoiceOptions{}, "", errors.New("--from and --to are required")
	}
	from, to, err := parseDateRange(*fromText, *toText)
	if err != nil {
		return app.InvoiceOptions{}, "", err
	}
	submitted, err := time.ParseInLocation("2006-01-02", *submittedText, time.Local)
	if err != nil {
		return app.InvoiceOptions{}, "", fmt.Errorf("submitted date: %w", err)
	}
	if *recipient < 1 {
		return app.InvoiceOptions{}, "", errors.New("recipient number must be positive")
	}
	options := app.InvoiceOptions{
		From: from, To: to, SubmittedDate: submitted, RecipientIndex: *recipient - 1,
		NumberOverride: *number, PayableTerms: *terms, Notes: *notes,
	}
	if includeProvided {
		options.IncludeIDs = []int64{}
		if *includeText != "" {
			options.IncludeIDs, err = parseIDs(*includeText)
			if err != nil {
				return app.InvoiceOptions{}, "", err
			}
		}
	}
	if *excludeText != "" {
		options.ExcludeIDs, err = parseIDs(*excludeText)
		if err != nil {
			return app.InvoiceOptions{}, "", err
		}
	}
	if *adjustmentText != "" {
		adjustment, err := domain.ParseMoney(*adjustmentText)
		if err != nil {
			return app.InvoiceOptions{}, "", fmt.Errorf("adjustment: %w", err)
		}
		options.AdjustmentCents = &adjustment
	}
	if contacts != nil {
		options.Contacts = make([]config.Contact, 0, len(contacts))
		for _, value := range contacts {
			parts := strings.SplitN(value, "|", 2)
			if len(parts) != 2 {
				return app.InvoiceOptions{}, "", errors.New("contacts must use Name|email")
			}
			options.Contacts = append(options.Contacts, config.Contact{Name: strings.TrimSpace(parts[0]), Email: strings.TrimSpace(parts[1])})
		}
	}
	return options, *outputDir, nil
}

// parseIDs converts a comma-separated entry selection to positive IDs.
func parseIDs(value string) ([]int64, error) {
	var ids []int64
	for _, item := range strings.Split(value, ",") {
		id, err := positiveID(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// printInvoicePreview displays selectable row IDs and calculated totals.
func printInvoicePreview(output io.Writer, currency string, preview app.InvoicePreview) {
	writer := tabwriter.NewWriter(output, 0, 2, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tDATE\tDESCRIPTION\tRATE\tUNITS\tTOTAL")
	for _, entry := range preview.Entries {
		fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%.2f\t%s\n", entry.ID, entry.StartAt.Format("2006-01-02"),
			entry.Description, domain.FormatMoney(entry.RateAmountCents), entry.Hours, domain.FormatMoney(entry.LineTotalCents()))
	}
	fmt.Fprintf(writer, "\t\t\t\tSubtotal\t%s %s\n", currency, domain.FormatMoney(preview.SubtotalCents))
	fmt.Fprintf(writer, "\t\t\t\tAdjustment\t%s %s\n", currency, domain.FormatMoney(preview.AdjustmentCents))
	fmt.Fprintf(writer, "\t\t\t\tTotal\t%s %s\n", currency, domain.FormatMoney(preview.TotalCents))
	writer.Flush()
}

// stringList collects repeatable flag values.
type stringList []string

// String renders repeatable values for flag help.
func (values *stringList) String() string { return strings.Join(*values, ",") }

// Set appends one repeatable flag value.
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

// rejectExtraArgs fails commands that received trailing positional arguments.
func rejectExtraArgs(flags *flag.FlagSet) error {
	if extra := flags.Args(); len(extra) > 0 {
		return fmt.Errorf("unexpected argument %q", extra[0])
	}
	return nil
}

// runConfig shows and edits the centralized TOML configuration.
func runConfig(application *app.App, args []string, output io.Writer) error {
	if len(args) == 0 || args[0] == "show" {
		printConfig(output, application.Config())
		return nil
	}
	cfg := application.Config()
	switch args[0] {
	case "set":
		if len(args) < 3 {
			return errors.New("usage: config set KEY VALUE")
		}
		value := strings.Join(args[2:], " ")
		switch args[1] {
		case "sender.name":
			cfg.Sender.FullName = value
		case "sender.address":
			cfg.Sender.Address = value
		case "sender.email":
			cfg.Sender.Email = value
		case "recipient.company":
			cfg.Recipients[0].CompanyName = value
		case "recipient.address":
			cfg.Recipients[0].Address = value
		case "terms":
			cfg.PayableTerms = value
		case "currency":
			cfg.Currency = value
		case "logo":
			cfg.LogoPath = value
		case "output":
			cfg.OutputDir = value
		case "notes":
			cfg.Notes = value
		case "adjustment":
			cfg.DefaultAdjustment = value
		default:
			return fmt.Errorf("unknown config key %q", args[1])
		}
	case "contact":
		if len(args) < 2 {
			return errors.New("usage: config contact add NAME EMAIL | delete NUMBER")
		}
		switch args[1] {
		case "add":
			if len(args) != 4 {
				return errors.New("usage: config contact add NAME EMAIL")
			}
			cfg.Contacts = append(cfg.Contacts, config.Contact{Name: args[2], Email: args[3]})
		case "delete":
			index, err := oneBasedIndex(args[2:], len(cfg.Contacts))
			if err != nil {
				return err
			}
			cfg.Contacts = append(cfg.Contacts[:index], cfg.Contacts[index+1:]...)
		default:
			return errors.New("usage: config contact add NAME EMAIL | delete NUMBER")
		}
	case "recipient":
		if len(args) < 2 {
			return errors.New("usage: config recipient add COMPANY ADDRESS | delete NUMBER")
		}
		switch args[1] {
		case "add":
			if len(args) != 4 {
				return errors.New("usage: config recipient add COMPANY ADDRESS")
			}
			cfg.Recipients = append(cfg.Recipients, config.Recipient{CompanyName: args[2], Address: args[3]})
		case "delete":
			if len(cfg.Recipients) == 1 {
				return errors.New("at least one recipient profile is required")
			}
			index, err := oneBasedIndex(args[2:], len(cfg.Recipients))
			if err != nil {
				return err
			}
			cfg.Recipients = append(cfg.Recipients[:index], cfg.Recipients[index+1:]...)
		default:
			return errors.New("usage: config recipient add COMPANY ADDRESS | delete NUMBER")
		}
	default:
		return errors.New("usage: config show | set | contact | recipient")
	}
	if err := config.Save(application.Paths.ConfigFile, cfg); err != nil {
		return err
	}
	application.SetConfig(cfg)
	fmt.Fprintln(output, "config saved")
	return nil
}

// runRate manages reusable unit prices.
func runRate(ctx context.Context, application *app.App, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: rate add|update|list|delete|restore")
	}
	switch args[0] {
	case "add":
		if len(args) < 3 || len(args) > 4 {
			return errors.New("usage: rate add LABEL AMOUNT [CURRENCY]")
		}
		amount, err := domain.ParseMoney(args[2])
		if err != nil || amount < 0 {
			return errors.New("rate amount must be a non-negative decimal")
		}
		currency := application.Config().Currency
		if len(args) == 4 {
			currency = args[3]
		}
		preset, err := application.Store.CreateRatePreset(ctx, domain.RatePreset{Label: args[1], AmountCents: amount, Currency: currency, Active: true})
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "created rate %d\n", preset.ID)
		return nil
	case "update":
		if len(args) < 4 || len(args) > 5 {
			return errors.New("usage: rate update ID LABEL AMOUNT [CURRENCY]")
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
		amount, err := domain.ParseMoney(args[3])
		if err != nil || amount < 0 {
			return errors.New("rate amount must be a non-negative decimal")
		}
		preset, err := findRate(ctx, application, id)
		if err != nil {
			return err
		}
		preset.Label, preset.AmountCents = args[2], amount
		if len(args) == 5 {
			preset.Currency = args[4]
		}
		_, err = application.Store.UpdateRatePreset(ctx, preset)
		return err
	case "list":
		includeInactive := len(args) == 2 && args[1] == "--all"
		presets, err := application.Store.ListRatePresets(ctx, includeInactive)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(output, 0, 2, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tLABEL\tAMOUNT\tCURRENCY\tACTIVE")
		for _, preset := range presets {
			fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%t\n", preset.ID, preset.Label, domain.FormatMoney(preset.AmountCents), preset.Currency, preset.Active)
		}
		return writer.Flush()
	case "delete", "restore":
		if len(args) != 2 {
			return fmt.Errorf("usage: rate %s ID", args[0])
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
		return application.Store.SetRatePresetActive(ctx, id, args[0] == "restore")
	default:
		return errors.New("usage: rate add|update|list|delete|restore")
	}
}

// runDescription manages reusable descriptions.
func runDescription(ctx context.Context, application *app.App, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: description add|update|list|delete|restore")
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return errors.New("usage: description add LABEL")
		}
		preset, err := application.Store.CreateDescriptionPreset(ctx, domain.DescriptionPreset{Label: args[1], Active: true})
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "created description %d\n", preset.ID)
		return nil
	case "update":
		if len(args) != 3 {
			return errors.New("usage: description update ID LABEL")
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
		presets, err := application.Store.ListDescriptionPresets(ctx, true)
		if err != nil {
			return err
		}
		for _, preset := range presets {
			if preset.ID == id {
				preset.Label = args[2]
				_, err = application.Store.UpdateDescriptionPreset(ctx, preset)
				return err
			}
		}
		return fmt.Errorf("description preset %d not found", id)
	case "list":
		includeInactive := len(args) == 2 && args[1] == "--all"
		presets, err := application.Store.ListDescriptionPresets(ctx, includeInactive)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(output, 0, 2, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tLABEL\tACTIVE")
		for _, preset := range presets {
			fmt.Fprintf(writer, "%d\t%s\t%t\n", preset.ID, preset.Label, preset.Active)
		}
		return writer.Flush()
	case "delete", "restore":
		if len(args) != 2 {
			return fmt.Errorf("usage: description %s ID", args[0])
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
		return application.Store.SetDescriptionPresetActive(ctx, id, args[0] == "restore")
	default:
		return errors.New("usage: description add|update|list|delete|restore")
	}
}

// runTime handles manual entry creation, editing, listing, and deletion.
func runTime(ctx context.Context, application *app.App, args []string, output, errorsOutput io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: time add|edit|list|delete")
	}
	switch args[0] {
	case "add":
		entry, err := parseTimeEntryFlags(ctx, application, args[1:], domain.TimeEntry{}, errorsOutput)
		if err != nil {
			return err
		}
		entry, err = application.Store.CreateTimeEntry(ctx, entry)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "created time entry %d (%0.2f hours)\n", entry.ID, entry.Hours)
		return nil
	case "edit":
		if len(args) < 2 {
			return errors.New("usage: time edit ID [options]")
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
		entry, err := application.Store.GetTimeEntry(ctx, id)
		if err != nil {
			return err
		}
		entry, err = parseTimeEntryFlags(ctx, application, args[2:], entry, errorsOutput)
		if err != nil {
			return err
		}
		_, err = application.Store.UpdateTimeEntry(ctx, entry)
		return err
	case "list":
		flags := flag.NewFlagSet("time list", flag.ContinueOnError)
		flags.SetOutput(errorsOutput)
		defaultFrom, defaultTo := defaultDateRange(time.Now())
		fromText := flags.String("from", defaultFrom, "first date")
		toText := flags.String("to", defaultTo, "last date")
		all := flags.Bool("all", false, "include invoiced entries")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := rejectExtraArgs(flags); err != nil {
			return err
		}
		from, to, err := parseDateRange(*fromText, *toText)
		if err != nil {
			return err
		}
		entries, err := application.Store.ListTimeEntries(ctx, from, to, !*all)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(output, 0, 2, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tSTART\tEND\tHOURS\tDESCRIPTION\tRATE\tTOTAL\tCURRENCY\tINVOICE")
		for _, entry := range entries {
			invoice := "-"
			if entry.InvoiceID != nil {
				invoice = strconv.FormatInt(*entry.InvoiceID, 10)
			}
			fmt.Fprintf(writer, "%d\t%s\t%s\t%.2f\t%s\t%s\t%s\t%s\t%s\n", entry.ID,
				entry.StartAt.Local().Format("2006-01-02 15:04"), entry.EndAt.Local().Format("2006-01-02 15:04"),
				entry.Hours, entry.Description, domain.FormatMoney(entry.RateAmountCents), domain.FormatMoney(entry.LineTotalCents()), entry.Currency, invoice)
		}
		return writer.Flush()
	case "delete":
		if len(args) != 2 {
			return errors.New("usage: time delete ID")
		}
		id, err := positiveID(args[1])
		if err != nil {
			return err
		}
		return application.Store.DeleteTimeEntry(ctx, id)
	default:
		return errors.New("usage: time add|edit|list|delete")
	}
}

// defaultDateRange returns local month-to-date values for time listings.
func defaultDateRange(now time.Time) (string, string) {
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return from.Format("2006-01-02"), now.Format("2006-01-02")
}

// parseTimeEntryFlags merges command flags into a new or existing entry.
func parseTimeEntryFlags(ctx context.Context, application *app.App, args []string, entry domain.TimeEntry, errorsOutput io.Writer) (domain.TimeEntry, error) {
	flags := flag.NewFlagSet("time entry", flag.ContinueOnError)
	flags.SetOutput(errorsOutput)
	start := flags.String("start", formatOptionalTime(entry.StartAt), "start datetime")
	end := flags.String("end", formatOptionalTime(entry.EndAt), "end datetime")
	description := flags.String("description", entry.Description, "description")
	rate := flags.String("rate", optionalMoney(entry), "unit rate")
	currency := flags.String("currency", defaultText(entry.Currency, application.Config().Currency), "currency")
	notes := flags.String("notes", entry.Notes, "notes")
	descriptionPreset := flags.Int64("description-preset", 0, "description preset ID")
	ratePreset := flags.Int64("rate-preset", 0, "rate preset ID")
	if err := flags.Parse(args); err != nil {
		return domain.TimeEntry{}, err
	}
	if err := rejectExtraArgs(flags); err != nil {
		return domain.TimeEntry{}, err
	}
	changed := map[string]bool{}
	flags.Visit(func(item *flag.Flag) { changed[item.Name] = true })
	var err error
	if entry.StartAt, err = parseDateTime(*start); err != nil {
		return domain.TimeEntry{}, fmt.Errorf("start: %w", err)
	}
	if entry.EndAt, err = parseDateTime(*end); err != nil {
		return domain.TimeEntry{}, fmt.Errorf("end: %w", err)
	}
	entry.Description, entry.Currency, entry.Notes = *description, *currency, *notes
	if changed["description-preset"] {
		if *descriptionPreset <= 0 {
			return domain.TimeEntry{}, errors.New("description preset ID must be positive")
		}
		presets, err := application.Store.ListDescriptionPresets(ctx, false)
		if err != nil {
			return domain.TimeEntry{}, err
		}
		found := false
		for _, preset := range presets {
			if preset.ID == *descriptionPreset {
				entry.Description, entry.DescriptionPresetID, found = preset.Label, &preset.ID, true
				break
			}
		}
		if !found {
			return domain.TimeEntry{}, fmt.Errorf("active description preset %d not found", *descriptionPreset)
		}
	} else if changed["description"] {
		entry.DescriptionPresetID = nil
	}
	if changed["rate-preset"] {
		if *ratePreset <= 0 {
			return domain.TimeEntry{}, errors.New("rate preset ID must be positive")
		}
		preset, err := findRate(ctx, application, *ratePreset)
		if err != nil || !preset.Active {
			return domain.TimeEntry{}, fmt.Errorf("active rate preset %d not found", *ratePreset)
		}
		entry.RateAmountCents, entry.Currency, entry.RatePresetID = preset.AmountCents, preset.Currency, &preset.ID
	} else if entry.ID == 0 || changed["rate"] {
		entry.RateAmountCents, err = domain.ParseMoney(*rate)
		if err != nil || entry.RateAmountCents < 0 {
			return domain.TimeEntry{}, errors.New("rate must be a non-negative decimal or --rate-preset must be set")
		}
		if changed["rate"] {
			entry.RatePresetID = nil
		}
	}
	return entry, nil
}

// printConfig renders all centralized fields without exposing implementation syntax.
func printConfig(output io.Writer, cfg config.Config) {
	fmt.Fprintf(output, "Sender: %s | %s | %s\n", cfg.Sender.FullName, cfg.Sender.Address, cfg.Sender.Email)
	for index, recipient := range cfg.Recipients {
		fmt.Fprintf(output, "Recipient %d: %s | %s\n", index+1, recipient.CompanyName, recipient.Address)
	}
	for index, contact := range cfg.Contacts {
		fmt.Fprintf(output, "Contact %d: %s | %s\n", index+1, contact.Name, contact.Email)
	}
	fmt.Fprintf(output, "Terms: %s\nCurrency: %s\nLogo: %s\nOutput: %s\nNotes: %s\nAdjustment: %s\n",
		cfg.PayableTerms, cfg.Currency, cfg.LogoPath, cfg.OutputDir, cfg.Notes, cfg.DefaultAdjustment)
}

// printHelp lists the command surface shared with the TUI console.
func printHelp(output io.Writer) {
	fmt.Fprintln(output, `ez-invoice commands:
  serve|web [ADDR]        start the web interface
  config show|set|contact|recipient
  rate add|update|list|delete|restore
  description add|update|list|delete|restore
  time add|edit|list|delete
  invoice preview|generate|list|export`)
}

// findRate returns one preset by ID.
func findRate(ctx context.Context, application *app.App, id int64) (domain.RatePreset, error) {
	presets, err := application.Store.ListRatePresets(ctx, true)
	if err != nil {
		return domain.RatePreset{}, err
	}
	for _, preset := range presets {
		if preset.ID == id {
			return preset, nil
		}
	}
	return domain.RatePreset{}, fmt.Errorf("rate preset %d not found", id)
}

// parseDateTime accepts RFC3339 or a local minute-precision datetime.
func parseDateTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02T15:04"} {
		var parsed time.Time
		var err error
		if layout == time.RFC3339 {
			parsed, err = time.Parse(layout, value)
		} else {
			parsed, err = time.ParseInLocation(layout, value, time.Local)
		}
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("use RFC3339 or YYYY-MM-DD HH:MM")
}

// parseDateRange turns inclusive calendar dates into a half-open range.
func parseDateRange(fromText, toText string) (time.Time, time.Time, error) {
	from, err := time.ParseInLocation("2006-01-02", fromText, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from date: %w", err)
	}
	to, err := time.ParseInLocation("2006-01-02", toText, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to date: %w", err)
	}
	to = to.AddDate(0, 0, 1)
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("to date must not be before from date")
	}
	return from, to, nil
}

// positiveID parses a database identifier.
func positiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid ID %q", value)
	}
	return id, nil
}

// oneBasedIndex validates a user-facing list number.
func oneBasedIndex(args []string, length int) (int, error) {
	if len(args) != 1 {
		return 0, errors.New("a list number is required")
	}
	number, err := strconv.Atoi(args[0])
	if err != nil || number < 1 || number > length {
		return 0, fmt.Errorf("number must be between 1 and %d", length)
	}
	return number - 1, nil
}

// formatOptionalTime keeps existing entry values during partial edits.
func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

// optionalMoney keeps an existing rate, including zero, during partial edits.
func optionalMoney(entry domain.TimeEntry) string {
	if entry.ID == 0 {
		return ""
	}
	return domain.FormatMoney(entry.RateAmountCents)
}

// defaultText chooses an entry value before the app default.
func defaultText(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
