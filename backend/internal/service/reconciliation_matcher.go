package service

import (
	"cmp"
	"slices"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// gatewayGroup is every gateway transaction of one reference, added up.
type gatewayGroup struct {
	side domain.GatewaySide
	// mixedCurrency is set when the transactions disagree on currency, so
	// their amounts cannot be added.
	mixedCurrency bool
}

// matchReconciliation compares KeiPay's payments with the gateway's
// transactions by reference and returns one item per reference whose
// anchor lies in [from, to), sorted by reference. The inputs may cover a
// wider window: the anchor decides which run an item belongs to, so a
// payment near a range edge lands in exactly one of two adjacent runs.
// Item public ids are left for the caller.
func matchReconciliation(from, to time.Time, keipay []domain.KeipayPayment, gateway []domain.GatewayTransaction) []domain.ReconciliationItem {
	groups := groupTransactions(gateway)
	payments := make(map[string]*domain.KeipayPayment, len(keipay))
	refs := make([]string, 0, len(groups)+len(keipay))
	for ref := range groups {
		refs = append(refs, ref)
	}
	for i := range keipay {
		k := &keipay[i]
		payments[k.Reference] = k
		if _, seen := groups[k.Reference]; !seen {
			refs = append(refs, k.Reference)
		}
	}
	slices.Sort(refs)

	items := make([]domain.ReconciliationItem, 0, len(refs))
	for _, ref := range refs {
		item, ok := matchReference(ref, payments[ref], groups[ref])
		if !ok || item.AnchorAt.Before(from) || !item.AnchorAt.Before(to) {
			continue
		}
		items = append(items, item)
	}
	return items
}

// matchReference applies the first rule that fits one reference. ok is
// false when there is nothing to report: an unsuccessful transaction KeiPay
// does not know.
func matchReference(ref string, k *domain.KeipayPayment, g *gatewayGroup) (item domain.ReconciliationItem, ok bool) {
	item.Reference = ref
	if k != nil {
		item.PaymentID = &k.PaymentID
		item.Keipay = &domain.KeipaySide{PaymentPublicID: k.PaymentPublicID, App: k.App, Status: k.Status,
			Amount: k.Amount, Currency: k.Currency, Fee: k.Fee, PaidAt: k.PaidAt}
	}
	if g != nil {
		side := g.side
		item.Gateway = &side
	}

	switch {
	case k == nil && g.side.Status.IsSuccessful():
		item.Result = domain.ResultMissingInKeipay
	case k == nil:
		return item, false
	case g == nil && k.Status.IsSuccessful():
		item.Result = domain.ResultMissingInGateway
	case g == nil:
		return item, false
	case g.mixedCurrency || k.Currency != g.side.Currency || k.Amount != g.side.Amount:
		item.Result = domain.ResultAmountMismatch
	case k.Status.IsSuccessful() != g.side.Status.IsSuccessful():
		item.Result = domain.ResultStatusMismatch
	case k.Status.IsSuccessful() && k.Fee != nil && g.side.Fee != nil && *k.Fee != *g.side.Fee:
		item.Result = domain.ResultFeeMismatch
	default:
		item.Result = domain.ResultMatched
	}

	switch {
	case k != nil && k.Status.IsSuccessful() && k.PaidAt != nil:
		item.AnchorAt = *k.PaidAt
	case g != nil:
		item.AnchorAt = g.side.OccurredAt
	default:
		return item, false
	}
	return item, true
}

// groupTransactions adds up the transactions of each reference. A group is
// successful when any transaction is (SETTLED when a successful one is);
// otherwise it has its earliest transaction's status. Amount and fee are the
// sums over the successful transactions, or over all of them when none
// succeeded, and the fee is unknown when any summed fee is.
func groupTransactions(txs []domain.GatewayTransaction) map[string]*gatewayGroup {
	byRef := map[string][]domain.GatewayTransaction{}
	for _, t := range txs {
		byRef[t.Reference] = append(byRef[t.Reference], t)
	}
	groups := make(map[string]*gatewayGroup, len(byRef))
	for ref, rows := range byRef {
		slices.SortFunc(rows, func(a, b domain.GatewayTransaction) int {
			if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
				return c
			}
			return cmp.Compare(a.ProviderPaymentID, b.ProviderPaymentID)
		})
		first := rows[0]
		g := &gatewayGroup{side: domain.GatewaySide{
			ProviderPaymentID: first.ProviderPaymentID, ProviderStatus: first.ProviderStatus, Status: first.Status,
			Currency: first.Currency, OccurredAt: first.OccurredAt, TransactionCount: len(rows),
		}}
		summed := make([]domain.GatewayTransaction, 0, len(rows))
		for _, r := range rows {
			if r.Currency != first.Currency {
				g.mixedCurrency = true
			}
			if r.Status.IsSuccessful() {
				summed = append(summed, r)
			}
		}
		if len(summed) > 0 {
			g.side.Status = domain.PaymentStatusPaid
			for _, r := range summed {
				if r.Status == domain.PaymentStatusSettled {
					g.side.Status = domain.PaymentStatusSettled
				}
			}
		} else {
			summed = rows
		}
		g.side.Amount, g.side.Fee = sumAmounts(summed)
		groups[ref] = g
	}
	return groups
}

// sumAmounts adds amounts and fees; the fee is nil when any is unknown.
func sumAmounts(rows []domain.GatewayTransaction) (amount int64, fee *int64) {
	var feeSum int64
	feeKnown := true
	for _, r := range rows {
		amount += r.Amount
		if r.Fee == nil {
			feeKnown = false
			continue
		}
		feeSum += *r.Fee
	}
	if feeKnown {
		fee = &feeSum
	}
	return amount, fee
}
