package domain

import "testing"

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
