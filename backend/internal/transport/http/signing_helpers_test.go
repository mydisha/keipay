package http

import (
	"strconv"
	"sync"
	"time"
)

// signedTimestamps remembers which (secret, timestamp, body) triples signed
// /v1 calls have used in this test process.
var signedTimestamps = struct {
	sync.Mutex
	used map[string]bool
}{used: map[string]bool{}}

// signedTimestamp returns an X-Timestamp inside the skew window that no
// earlier call with the same secret and body has used, so two identical
// requests (an idempotent replay, say) never share a signature and the
// replay guard stays out of the way. Subtracting a call counter from the
// current second is not enough: when the clock ticks between two calls, both
// land on the same timestamp.
func signedTimestamp(secret, body string) string {
	signedTimestamps.Lock()
	defer signedTimestamps.Unlock()
	now := time.Now().Unix()
	for back := int64(0); back < 240; back++ {
		ts := strconv.FormatInt(now-back, 10)
		k := secret + "\x00" + ts + "\x00" + body
		if !signedTimestamps.used[k] {
			signedTimestamps.used[k] = true
			return ts
		}
	}
	panic("signedTimestamp: more than 240 identical signed calls within four minutes")
}
