package service

import (
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

var (
	matchFrom = time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC) // 1 Oct 00:00 WIB
	matchTo   = time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC) // 2 Oct 00:00 WIB
	matchMid  = time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
)

func keipayRow(ref string, status domain.PaymentStatus, amount int64, fee *int64, paidAt *time.Time) domain.KeipayPayment {
	return domain.KeipayPayment{PaymentID: int64(len(ref)), PaymentPublicID: "pay_" + ref, Reference: ref, Status: status,
		Amount: amount, Currency: "IDR", Fee: fee, PaidAt: paidAt}
}

func gatewayRow(ref, txID string, status domain.PaymentStatus, amount int64, fee *int64, at time.Time) domain.GatewayTransaction {
	return domain.GatewayTransaction{Reference: ref, ProviderPaymentID: txID, ProviderStatus: string(status), Status: status,
		Amount: amount, Currency: "IDR", Fee: fee, OccurredAt: at}
}

func at(t time.Time) *time.Time { return &t }

// One case per rule, in precedence order.
func TestMatchResults(t *testing.T) {
	paid, settled, pending, failed, expired := domain.PaymentStatusPaid, domain.PaymentStatusSettled, domain.PaymentStatusPending,
		domain.PaymentStatusFailed, domain.PaymentStatusExpired
	usd := gatewayRow("r", "tx_1", paid, 5000, i64(100), matchMid)
	usd.Currency = "USD"
	unknown := gatewayRow("r", "tx_1", "", 5000, nil, matchMid)
	unknown.ProviderStatus = "WEIRD"
	cases := []struct {
		name    string
		keipay  []domain.KeipayPayment
		gateway []domain.GatewayTransaction
		want    domain.ReconciliationResult // "" means no item
		count   int                         // transaction_count, when a gateway side is expected
	}{
		{"matched", []domain.KeipayPayment{keipayRow("r", paid, 5000, i64(100), at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, i64(100), matchMid)}, domain.ResultMatched, 1},
		{"paid vs settled matches", []domain.KeipayPayment{keipayRow("r", paid, 5000, i64(100), at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", settled, 5000, i64(100), matchMid)}, domain.ResultMatched, 1},
		{"missing in keipay", nil,
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, nil, matchMid)}, domain.ResultMissingInKeipay, 1},
		{"gateway-only failed is no item", nil,
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", failed, 5000, nil, matchMid)}, "", 0},
		{"gateway-only unknown status is no item", nil, []domain.GatewayTransaction{unknown}, "", 0},
		{"missing in gateway", []domain.KeipayPayment{keipayRow("r", settled, 5000, nil, at(matchMid))}, nil,
			domain.ResultMissingInGateway, 0},
		{"keipay-only unpaid is no item", []domain.KeipayPayment{keipayRow("r", pending, 5000, nil, nil)}, nil, "", 0},
		{"amount differs", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5001, nil, matchMid)}, domain.ResultAmountMismatch, 1},
		{"currency differs", []domain.KeipayPayment{keipayRow("r", paid, 5000, i64(100), at(matchMid))},
			[]domain.GatewayTransaction{usd}, domain.ResultAmountMismatch, 1},
		{"double payment adds up", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, nil, matchMid),
				gatewayRow("r", "tx_2", paid, 5000, nil, matchMid.Add(time.Minute))}, domain.ResultAmountMismatch, 2},
		{"amount beats status", []domain.KeipayPayment{keipayRow("r", expired, 5000, nil, nil)},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 4000, nil, matchMid)}, domain.ResultAmountMismatch, 1},
		{"keipay pending, gateway paid", []domain.KeipayPayment{keipayRow("r", pending, 5000, nil, nil)},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, nil, matchMid)}, domain.ResultStatusMismatch, 1},
		{"keipay paid, gateway failed", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", failed, 5000, nil, matchMid)}, domain.ResultStatusMismatch, 1},
		{"keipay paid, gateway status unknown", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchMid))},
			[]domain.GatewayTransaction{unknown}, domain.ResultStatusMismatch, 1},
		{"fee differs", []domain.KeipayPayment{keipayRow("r", paid, 5000, i64(100), at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, i64(150), matchMid)}, domain.ResultFeeMismatch, 1},
		{"fee unknown at keipay", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, i64(150), matchMid)}, domain.ResultMatched, 1},
		{"fee unknown at gateway", []domain.KeipayPayment{keipayRow("r", paid, 5000, i64(100), at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", paid, 5000, nil, matchMid)}, domain.ResultMatched, 1},
		{"both unpaid agree", []domain.KeipayPayment{keipayRow("r", expired, 5000, nil, nil)},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", failed, 5000, nil, matchMid)}, domain.ResultMatched, 1},
		{"failed retry then success", []domain.KeipayPayment{keipayRow("r", paid, 5000, i64(100), at(matchMid))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx_1", failed, 5000, i64(1), matchMid.Add(-time.Minute)),
				gatewayRow("r", "tx_2", settled, 5000, i64(100), matchMid)}, domain.ResultMatched, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := matchReconciliation(matchFrom, matchTo, tc.keipay, tc.gateway)
			if tc.want == "" {
				if len(items) != 0 {
					t.Fatalf("items = %+v, want none", items)
				}
				return
			}
			if len(items) != 1 || items[0].Result != tc.want || items[0].Reference != "r" {
				t.Fatalf("items = %+v, want one %s", items, tc.want)
			}
			it := items[0]
			if (it.Keipay == nil) != (len(tc.keipay) == 0) || (it.Gateway == nil) != (len(tc.gateway) == 0) {
				t.Fatalf("sides = %+v / %+v", it.Keipay, it.Gateway)
			}
			if it.Gateway != nil && it.Gateway.TransactionCount != tc.count {
				t.Fatalf("transaction_count = %d, want %d", it.Gateway.TransactionCount, tc.count)
			}
			if it.Keipay != nil && (it.PaymentID == nil || it.Keipay.PaymentPublicID != "pay_r") {
				t.Fatalf("keipay side = %+v, payment %v", it.Keipay, it.PaymentID)
			}
		})
	}
}

// A group sums its successful transactions, takes the earliest one's
// id, and is SETTLED when any successful one is.
func TestMatchGroupsDuplicateTransactions(t *testing.T) {
	items := matchReconciliation(matchFrom, matchTo, nil, []domain.GatewayTransaction{
		gatewayRow("r", "tx_b", domain.PaymentStatusPaid, 3000, i64(30), matchMid.Add(time.Minute)),
		gatewayRow("r", "tx_c", domain.PaymentStatusFailed, 9999, nil, matchMid.Add(2*time.Minute)),
		gatewayRow("r", "tx_a", domain.PaymentStatusSettled, 2000, i64(20), matchMid),
	})
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	g := items[0].Gateway
	if g.Status != domain.PaymentStatusSettled || g.Amount != 5000 || g.Fee == nil || *g.Fee != 50 ||
		g.ProviderPaymentID != "tx_a" || g.TransactionCount != 3 || !g.OccurredAt.Equal(matchMid) {
		t.Fatalf("gateway side = %+v (fee %v)", g, g.Fee)
	}

	// Without a successful transaction, all are summed and the earliest
	// status stands; one unknown fee makes the sum unknown.
	items = matchReconciliation(matchFrom, matchTo, []domain.KeipayPayment{keipayRow("r", domain.PaymentStatusPending, 3000, nil, nil)},
		[]domain.GatewayTransaction{
			gatewayRow("r", "tx_2", domain.PaymentStatusFailed, 1000, nil, matchMid.Add(time.Minute)),
			gatewayRow("r", "tx_1", domain.PaymentStatusPending, 2000, i64(5), matchMid),
		})
	g = items[0].Gateway
	if g.Status != domain.PaymentStatusPending || g.Amount != 3000 || g.Fee != nil || items[0].Result != domain.ResultMatched {
		t.Fatalf("unsuccessful group = %+v, result %s", g, items[0].Result)
	}
}

// Mixed currencies within one reference cannot be added up.
func TestMatchMixedCurrencyGroupIsAmountMismatch(t *testing.T) {
	usd := gatewayRow("r", "tx_2", domain.PaymentStatusPaid, 2500, nil, matchMid.Add(time.Minute))
	usd.Currency = "USD"
	items := matchReconciliation(matchFrom, matchTo,
		[]domain.KeipayPayment{keipayRow("r", domain.PaymentStatusPaid, 5000, nil, at(matchMid))},
		[]domain.GatewayTransaction{gatewayRow("r", "tx_1", domain.PaymentStatusPaid, 2500, nil, matchMid), usd})
	if len(items) != 1 || items[0].Result != domain.ResultAmountMismatch || items[0].Gateway.Currency != "IDR" {
		t.Fatalf("items = %+v", items)
	}
}

// Empty inputs give nothing; odd references survive as they are and
// the output is sorted by reference.
func TestMatchEmptyAndOrdering(t *testing.T) {
	if items := matchReconciliation(matchFrom, matchTo, nil, nil); len(items) != 0 {
		t.Fatalf("empty = %+v", items)
	}
	refs := []string{"kp_pay_B", "acme:ORDER-ü/1", "kp_pay_A", "Ωmega 1"}
	var gw []domain.GatewayTransaction
	for i, ref := range refs {
		gw = append(gw, gatewayRow(ref, "tx_"+ref, domain.PaymentStatusPaid, int64(1000+i), nil, matchMid))
	}
	items := matchReconciliation(matchFrom, matchTo,
		[]domain.KeipayPayment{keipayRow("acme:ORDER-ü/1", domain.PaymentStatusPaid, 1001, nil, at(matchMid))}, gw)
	want := []string{"acme:ORDER-ü/1", "kp_pay_A", "kp_pay_B", "Ωmega 1"}
	if len(items) != len(want) {
		t.Fatalf("items = %+v", items)
	}
	for i, ref := range want {
		if items[i].Reference != ref {
			t.Fatalf("item %d = %q, want %q", i, items[i].Reference, ref)
		}
	}
	if items[0].Result != domain.ResultMatched || items[1].Result != domain.ResultMissingInKeipay {
		t.Fatalf("results = %s, %s", items[0].Result, items[1].Result)
	}
}

// The anchor (KeiPay's paid time, else the gateway's) decides
// which run an item belongs to; the range is half-open.
func TestMatchAnchorWindow(t *testing.T) {
	paid := domain.PaymentStatusPaid
	cases := []struct {
		name    string
		keipay  []domain.KeipayPayment
		gateway []domain.GatewayTransaction
		want    domain.ReconciliationResult
	}{
		{"paid 1s before to, gateway 30s after to: one matched item",
			[]domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchTo.Add(-time.Second)))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchTo.Add(30*time.Second))}, domain.ResultMatched},
		{"gateway-only 10 min before from: excluded", nil,
			[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchFrom.Add(-10*time.Minute))}, ""},
		{"gateway-only exactly at from: included", nil,
			[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchFrom)}, domain.ResultMissingInKeipay},
		{"gateway-only exactly at to: excluded", nil,
			[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchTo)}, ""},
		{"keipay-only exactly at from: included", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchFrom))}, nil,
			domain.ResultMissingInGateway},
		{"keipay-only exactly at to: excluded", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchTo))}, nil, ""},
		{"keipay paid after to wins over gateway inside", []domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchTo.Add(time.Second)))},
			[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchTo.Add(-time.Second))}, ""},
		{"unpaid keipay anchors on the gateway", []domain.KeipayPayment{keipayRow("r", domain.PaymentStatusExpired, 5000, nil, nil)},
			[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchFrom.Add(time.Minute))}, domain.ResultStatusMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := matchReconciliation(matchFrom, matchTo, tc.keipay, tc.gateway)
			if tc.want == "" {
				if len(items) != 0 {
					t.Fatalf("items = %+v, want none", items)
				}
				return
			}
			if len(items) != 1 || items[0].Result != tc.want {
				t.Fatalf("items = %+v, want %s", items, tc.want)
			}
		})
	}

	// The next day's run sees the late gateway row's payment exactly once:
	// not as missing on either side.
	next := matchReconciliation(matchTo, matchTo.Add(24*time.Hour),
		[]domain.KeipayPayment{keipayRow("r", paid, 5000, nil, at(matchTo.Add(-time.Second)))},
		[]domain.GatewayTransaction{gatewayRow("r", "tx", paid, 5000, nil, matchTo.Add(30*time.Second))})
	if len(next) != 0 {
		t.Fatalf("next day's run = %+v, want nothing", next)
	}
}
