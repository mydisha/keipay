package handler

import (
	"testing"
	"time"
)

// Text cells that a spreadsheet would run as a formula get a leading quote;
// everything else is left alone.
func TestCSVText(t *testing.T) {
	cases := map[string]string{
		"=cmd":      "'=cmd",
		"+1":        "'+1",
		"-x":        "'-x",
		"@a":        "'@a",
		"\tx":       "'\tx",
		"\rx":       "'\rx",
		"":          "",
		"kp_pay_A":  "kp_pay_A",
		"a=b":       "a=b",
		"ü-order":   "ü-order",
		" =padded":  " =padded",
		"'=already": "'=already",
	}
	for in, want := range cases {
		if got := csvText(in); got != want {
			t.Errorf("csvText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSVCells(t *testing.T) {
	at := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC)
	if got := csvTime(&at); got != "2026-11-01T00:00:00+07:00" {
		t.Fatalf("csvTime = %q", got)
	}
	if csvTime(nil) != "" || csvInt(nil) != "" || csvDate(nil) != "all" {
		t.Fatal("empty cells")
	}
	n := int64(-5)
	if csvInt(&n) != "-5" {
		t.Fatalf("csvInt = %q, amounts are never guarded", csvInt(&n))
	}
	if got := csvDate(&at); got != "20261101" {
		t.Fatalf("csvDate = %q", got)
	}
}
