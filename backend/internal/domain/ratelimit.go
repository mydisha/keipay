package domain

import (
	"context"
	"time"
)

// FailureLimiter blocks a caller whose recent failures reached a limit. Unlike
// FailureCounter it counts after the fact: a request is checked against the
// counters first and counted only when it fails, so concurrent legitimate
// requests never hold a slot. The overrun that allows is bounded by how many
// failures run at once, which is enough against brute force and replay.
type FailureLimiter interface {
	// Blocked reports whether any of the limits is reached, with the longest
	// time left among the exceeded windows. A counter found without an
	// expiry is given one of window, so it cannot block forever.
	Blocked(ctx context.Context, window time.Duration, limits ...FailureLimit) (bool, time.Duration, error)
	// Fail counts one failure under every key. A key's first failure starts a
	// window of the given length.
	Fail(ctx context.Context, window time.Duration, keys ...string) error
}
