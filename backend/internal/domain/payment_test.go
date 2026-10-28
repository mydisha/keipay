package domain

import "testing"

func TestPaymentStatusCanTransitionTo(t *testing.T) {
	const (
		pending = PaymentStatusPending
		paid    = PaymentStatusPaid
		settled = PaymentStatusSettled
		expired = PaymentStatusExpired
		failed  = PaymentStatusFailed
	)
	all := []PaymentStatus{pending, paid, settled, expired, failed}
	allowed := map[[2]PaymentStatus]bool{
		{pending, paid}: true, {pending, settled}: true, {pending, expired}: true, {pending, failed}: true,
		{paid, settled}: true,
		{expired, paid}: true, {expired, settled}: true,
		{failed, paid}: true, {failed, settled}: true,
	}
	for _, from := range all {
		for _, to := range all {
			want := allowed[[2]PaymentStatus{from, to}]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}
	if PaymentStatus("WEIRD").CanTransitionTo(paid) {
		t.Error("unknown current status should not transition")
	}
}
