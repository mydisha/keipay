package domain

import (
	"strconv"
	"testing"
)

// Amounts convert exactly, with the currency's exponent.
func TestMinorUnitsConversion(t *testing.T) {
	cases := []struct {
		in, currency string
		want         int64
	}{
		{"150000", "IDR", 150000},
		{"16777217", "IDR", 16777217},
		{"150000.5", "IDR", 150001},
		{"150000.49", "IDR", 150000},
		{"12.345", "USD", 1235},
		{"12.344", "USD", 1234},
		{"-12.345", "USD", -1235},
		{"1.5e2", "SGD", 15000},
		{"10", "VND", 10},
		{"10", "XYZ", 1000},
	}
	for _, tc := range cases {
		got, err := MinorUnits(tc.in, tc.currency)
		if err != nil || got != tc.want {
			t.Errorf("MinorUnits(%s, %s) = %d, %v; want %d", tc.in, tc.currency, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "abc", "1e999999999", "1/3", "99999999999999999999"} {
		if _, err := MinorUnits(bad, "IDR"); err == nil {
			t.Errorf("MinorUnits(%q) succeeded", bad)
		}
	}
}

// Minor amounts convert back to the major decimal gateways expect.
func TestMajorUnitsConversion(t *testing.T) {
	cases := []struct {
		minor    int64
		currency string
		want     float64
	}{
		{150000, "IDR", 150000},
		{16777217, "IDR", 16777217},
		{1050, "USD", 10.5},
		{1999, "usd", 19.99},
		{0, "SGD", 0},
		{-1235, "USD", -12.35},
		{10, "VND", 10},
		{1000, "XYZ", 10},
	}
	for _, tc := range cases {
		if got := MajorUnits(tc.minor, tc.currency); got != tc.want {
			t.Errorf("MajorUnits(%d, %s) = %v; want %v", tc.minor, tc.currency, got, tc.want)
		}
	}
	for _, minor := range []int64{1, 99, 1999, 123456789} {
		back, err := MinorUnits(strconv.FormatFloat(MajorUnits(minor, "USD"), 'f', -1, 64), "USD")
		if err != nil || back != minor {
			t.Errorf("round trip %d = %d, %v", minor, back, err)
		}
	}
}
