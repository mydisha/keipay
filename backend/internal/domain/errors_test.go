package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodedErrorMatching(t *testing.T) {
	wrapped := fmt.Errorf("list: %w", ErrInvalidCursor)
	if !errors.Is(wrapped, ErrInvalidCursor) || !errors.Is(wrapped, ErrInvalidInput) {
		t.Fatalf("wrapped invalid cursor lost its identity: %v", wrapped)
	}
	other := NewError(CodeInvalidCursor, "a different message")
	if !errors.Is(other, ErrInvalidCursor) {
		t.Fatal("errors with the same code should match")
	}
	if errors.Is(NewError(CodeConflict, "x"), ErrInvalidCursor) {
		t.Fatal("errors with different codes matched")
	}
	var coded *Error
	if !errors.As(fmt.Errorf("login: %w", ErrInvalidCredentials), &coded) || coded.Code != CodeInvalidCredentials {
		t.Fatalf("errors.As = %+v", coded)
	}
	if !errors.Is(ErrInvalidCredentials, ErrUnauthorized) {
		t.Fatal("invalid credentials should still be ErrUnauthorized")
	}
	if !errors.Is(Internal(fmt.Errorf("x: %w", ErrNotFound)), ErrNotFound) {
		t.Fatal("Internal should keep the cause")
	}
}

func TestSentinelsSurviveWrapping(t *testing.T) {
	for _, s := range []error{ErrNotFound, ErrConflict, ErrInvalidInput, ErrUnauthorized, ErrAppDisabled, ErrDuplicateOrderID} {
		if err := fmt.Errorf("%w: detail", s); !errors.Is(err, s) {
			t.Errorf("errors.Is(%v, %v) = false", err, s)
		}
	}
}

func TestErrorCodesAreUnique(t *testing.T) {
	seen := map[ErrorCode]bool{}
	for _, c := range ErrorCodes() {
		if seen[c] {
			t.Fatalf("duplicate code %s", c)
		}
		seen[c] = true
	}
	if len(seen) != 38 {
		t.Fatalf("got %d codes, want 38", len(seen))
	}
}

func TestRunInProgress(t *testing.T) {
	err := RunInProgress()
	if err.Code != "run_in_progress" || !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %+v", err)
	}
}

func TestDeliveryNotReplayable(t *testing.T) {
	err := DeliveryNotReplayable(ReplayReasonDisabled)
	if err.Code != CodeDeliveryNotReplayable || err.Details["reason"] != "endpoint_disabled" || !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %+v", err)
	}
}
