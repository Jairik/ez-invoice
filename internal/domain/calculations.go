// Package domain contains invoice and time-entry business rules.
package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Hours returns the billable duration between two timestamps.
func Hours(start, end time.Time) (float64, error) {
	if !end.After(start) {
		return 0, errors.New("end time must be after start time")
	}
	return math.Round(end.Sub(start).Hours()*100) / 100, nil
}

// LineTotal returns a rate multiplied by its units in cents.
func LineTotal(rateCents int64, units float64) (int64, error) {
	if rateCents < 0 || units < 0 || math.IsNaN(units) || math.IsInf(units, 0) {
		return 0, errors.New("rate and units must be non-negative finite values")
	}
	total := float64(rateCents) * units
	if total >= 1<<63 {
		return 0, errors.New("line total is too large")
	}
	return int64(math.Round(total)), nil
}

// ParseMoney converts a decimal amount to cents.
func ParseMoney(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("amount is required")
	}

	sign := int64(1)
	if value[0] == '-' || value[0] == '+' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || value == "" || len(parts) == 2 && len(parts[1]) > 2 {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	if parts[0] == "" {
		parts[0] = "0"
	}
	if len(parts) == 1 {
		parts = append(parts, "")
	}
	for len(parts[1]) < 2 {
		parts[1] += "0"
	}
	cents, err := strconv.ParseInt(parts[0]+parts[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", value, err)
	}
	return sign * cents, nil
}

// FormatMoney converts cents to a two-decimal amount.
func FormatMoney(cents int64) string {
	whole, fraction := cents/100, cents%100
	if fraction < 0 {
		fraction = -fraction
	}
	if cents < 0 && whole == 0 {
		return fmt.Sprintf("-0.%02d", fraction)
	}
	return fmt.Sprintf("%d.%02d", whole, fraction)
}
