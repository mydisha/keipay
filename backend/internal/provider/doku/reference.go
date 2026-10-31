package doku

import (
	"fmt"
	"strings"

	"github.com/mydisha/keipay/backend/internal/provider"
)

// KeiPay's payment references are "kp_pay_" and a 26-character ULID, 33
// characters with underscores. DOKU allows an invoice_number of at most 30
// characters once cards are enabled and no symbols for KKI, so the reference
// travels as "KP" and the ULID instead and is mapped back on the way in.
const (
	referencePrefix = "kp_pay_"
	invoicePrefix   = "KP"
	ulidLength      = 26
	// maxInvoiceNumber is DOKU's limit without cards.
	maxInvoiceNumber = 64
)

// invoiceNumber is the invoice_number a payment reference travels as. A
// reference of another form is sent as it is, within DOKU's limit.
func invoiceNumber(reference string) (string, error) {
	if rest, ok := strings.CutPrefix(reference, referencePrefix); ok && isULID(rest) {
		return invoicePrefix + rest, nil
	}
	if reference == "" || len(reference) > maxInvoiceNumber {
		return "", fmt.Errorf("%w: reference %q cannot be a DOKU invoice number", provider.ErrRejected, reference)
	}
	return reference, nil
}

// reference is the payment reference an invoice_number stands for.
func reference(invoice string) string {
	if rest, ok := strings.CutPrefix(invoice, invoicePrefix); ok && isULID(rest) {
		return referencePrefix + rest
	}
	return invoice
}

// isULID reports whether s looks like an upper-case ULID: 26 Crockford
// base32 characters.
func isULID(s string) bool {
	if len(s) != ulidLength {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}
