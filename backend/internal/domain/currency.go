package domain

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// currencyExponents are the ISO 4217 minor-unit exponents of the currencies
// the gateways KeiPay supports settle in.
var currencyExponents = map[string]int{
	"IDR": 0,
	"VND": 0,
	"USD": 2,
	"PHP": 2,
	"SGD": 2,
	"MYR": 2,
	"THB": 2,
}

// decimalRe is a JSON number whose exponent has at most two digits, so a
// hostile "1e999999999" cannot make the parser build a huge integer.
var decimalRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]{1,2})?$`)

// defaultCurrencyExponent is used for a currency missing from the table; most
// currencies have two decimals.
const defaultCurrencyExponent = 2

// CurrencyExponent is the number of decimals of currency's minor unit.
func CurrencyExponent(currency string) int {
	if e, ok := currencyExponents[strings.ToUpper(currency)]; ok {
		return e
	}
	return defaultCurrencyExponent
}

// MinorUnits converts a decimal amount in major units, as gateways send it
// (e.g. "150000" or "12.345"), to an integer in currency's minor unit,
// rounding half away from zero. The decimal is parsed exactly, so no amount
// loses precision on the way.
func MinorUnits(decimal, currency string) (int64, error) {
	decimal = strings.TrimSpace(decimal)
	if !decimalRe.MatchString(decimal) {
		return 0, fmt.Errorf("%w: amount %q is not a decimal number", ErrInvalidInput, decimal)
	}
	r, ok := new(big.Rat).SetString(decimal)
	if !ok {
		return 0, fmt.Errorf("%w: amount %q is not a number", ErrInvalidInput, decimal)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(CurrencyExponent(currency))), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))

	num, den := new(big.Int).Abs(r.Num()), r.Denom()
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Lsh(rem, 1).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if r.Sign() < 0 {
		q.Neg(q)
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("%w: amount %q is out of range", ErrInvalidInput, decimal)
	}
	return q.Int64(), nil
}
