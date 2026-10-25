package payouts

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"

	api "github.com/todayz/backend/gen/api/admin-payouts"
)

// csvWriter writes formula-safe rows (D-93): a cell a spreadsheet would read
// as a formula (=, +, -, @, a tab or a carriage return first) gets a leading
// apostrophe. Amount cells are written raw by rowRaw, never through it, so a
// negative amount stays a number.
type csvWriter struct{ w *csv.Writer }

func newCSV(b *bytes.Buffer) *csvWriter { return &csvWriter{w: csv.NewWriter(b)} }

func (c *csvWriter) row(cells ...string) {
	safe := make([]string, len(cells))
	for i, s := range cells {
		safe[i] = safeCell(s)
	}
	_ = c.w.Write(safe) // a bytes.Buffer never fails
	c.w.Flush()
}

// rowRaw writes text cells safely and numeric cells as they are; numeric[i]
// marks a cell this package formatted from an integer.
func (c *csvWriter) rowRaw(cells []string, numeric []bool) {
	out := make([]string, len(cells))
	for i, s := range cells {
		if numeric[i] {
			out[i] = s
		} else {
			out[i] = safeCell(s)
		}
	}
	_ = c.w.Write(out)
	c.w.Flush()
}

func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// inr formats paise as rupees with two decimals ("2670.00", "-30.00").
func inr(p int) string {
	sign := ""
	if p < 0 {
		sign, p = "-", -p
	}
	frac := strconv.Itoa(p % 100)
	if len(frac) == 1 {
		frac = "0" + frac
	}
	return sign + strconv.Itoa(p/100) + "." + frac
}

// statementCSV is a statement's per-store CSV (the contract's example).
func statementCSV(period string, stores []api.StatementStore) []byte {
	var b bytes.Buffer
	w := newCSV(&b)
	w.row("period", "store_id", "store_name", "opening_inr", "credits_in_inr", "referral_in_inr", "cashback_out_inr",
		"reversals_inr", "adjustments_inr", "payouts_out_inr", "closing_inr", "entries")
	num := []bool{false, false, false, true, true, true, true, true, true, true, true, true}
	for _, s := range stores {
		t := s.Totals
		w.rowRaw([]string{period, s.StoreId, s.StoreName, inr(t.OpeningPaise), inr(t.CreditsInPaise), inr(t.ReferralInPaise),
			inr(t.CashbackOutPaise), inr(t.ReversalsPaise), inr(t.AdjustmentsPaise), inr(t.PayoutsOutPaise), inr(t.ClosingPaise),
			strconv.Itoa(s.Entries)}, num)
	}
	return b.Bytes()
}
