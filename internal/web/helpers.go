package web

import (
	"errors"
	"strconv"
	"time"

	"github.com/Jairik/ez-invoice/internal/config"
)

// parseDateRange turns inclusive calendar dates into a half-open range.
func parseDateRange(fromText, toText string, now time.Time) (time.Time, time.Time, error) {
	from, err := time.ParseInLocation("2006-01-02", fromText, time.Local)
	if err != nil {
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
	to, err := time.ParseInLocation("2006-01-02", toText, time.Local)
	if err != nil {
		to = now
	}
	to = to.AddDate(0, 0, 1)
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("to date must not be before from date")
	}
	return from, to, nil
}

// parseID validates a positive path identifier.
func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid ID")
	}
	return id, nil
}

// configSnapshot is the editable settings view.
type configSnapshot config.Config
