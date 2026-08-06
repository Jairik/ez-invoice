// Package pdf renders finalized invoice snapshots directly to PDF files.
package pdf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/Jairik/ez-invoice/internal/domain"
)

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// Filename returns a safe deterministic filename for an invoice.
func Filename(invoice domain.Invoice) string {
	number := invoice.DisplayNumber()
	if number == "" {
		number = fmt.Sprintf("id-%d", invoice.ID)
	}
	number = strings.Trim(unsafeFilename.ReplaceAllString(number, "-"), "-")
	if number == "" {
		number = "invoice"
	}
	return "invoice-" + number + ".pdf"
}

// Render writes a finalized invoice to a PDF file.
func Render(invoice domain.Invoice, path string) error {
	if len(invoice.LineItems) == 0 {
		return fmt.Errorf("invoice has no line items")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create PDF directory: %w", err)
	}

	document := fpdf.New("P", "mm", "Letter", "")
	document.SetCompression(false)
	document.SetMargins(15, 15, 15)
	document.SetAutoPageBreak(true, 18)
	document.AddPage()
	renderHeader(document, invoice)
	if document.Err() {
		return fmt.Errorf("render PDF header: %w", document.Error())
	}
	renderParties(document, invoice)
	renderLineItems(document, invoice)
	renderTotals(document, invoice)
	renderFooter(document, invoice)
	if err := document.OutputFileAndClose(path); err != nil {
		return fmt.Errorf("write PDF: %w", err)
	}
	return nil
}

// renderHeader draws the logo, title, submission date, and sender block.
func renderHeader(document *fpdf.Fpdf, invoice domain.Invoice) {
	if invoice.LogoPath != "" {
		document.Image(invoice.LogoPath, 137, 15, 64, 0, false, "", 0, "")
	}
	document.SetTextColor(19, 66, 101)
	document.SetFont("Arial", "B", 24)
	document.SetXY(15, 15)
	document.CellFormat(105, 10, "Invoice", "", 1, "L", false, 0, "")
	document.SetTextColor(128, 128, 128)
	document.SetFont("Arial", "B", 10)
	document.SetXY(15, 28)
	document.CellFormat(105, 5, "Submitted on "+invoice.SubmittedDate.Format("01/02/2006"), "", 1, "L", false, 0, "")
	document.SetTextColor(70, 70, 70)
	document.SetFont("Arial", "B", 10)
	document.SetXY(15, 38)
	document.CellFormat(105, 6, "Invoice From", "", 1, "L", false, 0, "")
	document.SetTextColor(0, 0, 0)
	document.SetFont("Arial", "", 10)
	document.SetXY(15, 44)
	document.MultiCell(105, 5, strings.Join([]string{invoice.FromName, invoice.FromAddress, invoice.FromEmail}, "\n"), "", "L", false)
	document.SetY(document.GetY() + 7)
}

// renderParties lays out the recipient, contacts, number, terms, and period.
func renderParties(document *fpdf.Fpdf, invoice domain.Invoice) {
	y := document.GetY()
	contactLines := make([]string, 0, len(invoice.Contacts)*2)
	for _, contact := range invoice.Contacts {
		contactLines = append(contactLines, contact.Name, contact.Email)
	}

	document.SetTextColor(70, 70, 70)
	document.SetFont("Arial", "B", 10)
	document.SetXY(15, y)
	document.CellFormat(65, 6, "Invoice To", "", 0, "L", false, 0, "")
	document.SetXY(90, y)
	document.CellFormat(65, 6, "Point of Contact", "", 0, "L", false, 0, "")
	document.SetXY(160, y)
	document.CellFormat(41, 6, "Invoice #", "", 1, "L", false, 0, "")

	bodyY := y + 8
	document.SetTextColor(0, 0, 0)
	document.SetFont("Arial", "", 9)
	document.SetXY(15, bodyY)
	document.MultiCell(65, 5, strings.Join([]string{invoice.ToCompany, invoice.ToAddress}, "\n"), "", "L", false)
	toBottom := document.GetY()
	document.SetXY(90, bodyY)
	document.MultiCell(65, 5, strings.Join(contactLines, "\n"), "", "L", false)
	contactsBottom := document.GetY()
	document.SetXY(160, bodyY)
	document.CellFormat(41, 5, invoice.DisplayNumber(), "", 1, "L", false, 0, "")

	secondaryY := toBottom
	if contactsBottom > secondaryY {
		secondaryY = contactsBottom
	}
	secondaryY += 4
	document.SetTextColor(70, 70, 70)
	document.SetFont("Arial", "B", 10)
	document.SetXY(90, secondaryY)
	document.CellFormat(65, 6, "Payable", "", 0, "L", false, 0, "")
	document.SetXY(160, secondaryY)
	document.CellFormat(41, 6, "Period", "", 1, "L", false, 0, "")
	document.SetTextColor(0, 0, 0)
	document.SetFont("Arial", "", 9)
	document.SetXY(90, secondaryY+8)
	document.CellFormat(65, 5, invoice.PayableTerms, "", 1, "L", false, 0, "")
	document.SetXY(160, secondaryY+8)
	document.CellFormat(41, 5, invoice.PeriodStart.Format("1/2")+" - "+invoice.PeriodEnd.Format("1/2"), "", 1, "L", false, 0, "")
	document.SetY(secondaryY + 18)
}

// renderLineItems draws a compact table and repeats its header after page breaks.
func renderLineItems(document *fpdf.Fpdf, invoice domain.Invoice) {
	renderTableHeader(document)
	document.SetFont("Arial", "", 9)
	for _, item := range invoice.LineItems {
		lines := document.SplitLines([]byte(strings.ReplaceAll(item.Description, "\n", " ")), 83)
		rowHeight := float64(len(lines)) * 5
		if rowHeight < 7 {
			rowHeight = 7
		}
		_, pageHeight := document.GetPageSize()
		if document.GetY()+rowHeight > pageHeight-18 {
			document.AddPage()
			renderTableHeader(document)
			document.SetFont("Arial", "", 9)
		}
		x, y := document.GetXY()
		document.MultiCell(85, rowHeight/float64(len(lines)), strings.ReplaceAll(item.Description, "\n", " "), "B", "L", false)
		document.SetXY(x+85, y)
		document.CellFormat(35, rowHeight, money(invoice.Currency, item.UnitPriceCents), "B", 0, "R", false, 0, "")
		document.CellFormat(25, rowHeight, fmt.Sprintf("%.2f", item.Units), "B", 0, "R", false, 0, "")
		document.CellFormat(40, rowHeight, money(invoice.Currency, item.LineTotalCents), "B", 0, "R", false, 0, "")
		document.SetXY(x, y+rowHeight)
	}
}

// renderTableHeader draws invoice row column names.
func renderTableHeader(document *fpdf.Fpdf) {
	document.SetFillColor(235, 238, 242)
	document.SetFont("Arial", "B", 9)
	document.CellFormat(85, 7, "Description", "TB", 0, "L", true, 0, "")
	document.CellFormat(35, 7, "Price per unit", "TB", 0, "R", true, 0, "")
	document.CellFormat(25, 7, "Units", "TB", 0, "R", true, 0, "")
	document.CellFormat(40, 7, "Total price", "TB", 1, "R", true, 0, "")
}

// renderTotals shows subtotal, adjustment, and final total.
func renderTotals(document *fpdf.Fpdf, invoice domain.Invoice) {
	document.Ln(5)
	document.SetFont("Arial", "", 10)
	document.CellFormat(145, 6, "Subtotal", "", 0, "R", false, 0, "")
	document.CellFormat(40, 6, money(invoice.Currency, invoice.SubtotalCents), "", 1, "R", false, 0, "")
	document.CellFormat(145, 6, "Adjustment", "", 0, "R", false, 0, "")
	document.CellFormat(40, 6, money(invoice.Currency, invoice.AdjustmentCents), "", 1, "R", false, 0, "")
	document.SetFont("Arial", "B", 12)
	document.CellFormat(145, 8, "Total", "T", 0, "R", false, 0, "")
	document.CellFormat(40, 8, money(invoice.Currency, invoice.TotalCents), "T", 1, "R", false, 0, "")
}

// renderFooter writes the notes section.
func renderFooter(document *fpdf.Fpdf, invoice domain.Invoice) {
	document.Ln(8)
	document.SetFont("Arial", "B", 10)
	document.CellFormat(0, 6, "Notes", "B", 1, "L", false, 0, "")
	document.SetFont("Arial", "", 10)
	document.MultiCell(0, 5, invoice.Notes, "", "L", false)
}

// money formats a snapshotted integer-cent amount with its currency label.
func money(currency string, cents int64) string {
	return currency + " " + domain.FormatMoney(cents)
}
