package doku

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

// Headers of DOKU's non-SNAP signature scheme, used on requests to DOKU and
// on the notifications DOKU sends.
const (
	headerClientID         = "Client-Id"
	headerRequestID        = "Request-Id"
	headerRequestTimestamp = "Request-Timestamp"
	headerSignature        = "Signature"
)

// signaturePrefix names the algorithm in the Signature header.
const signaturePrefix = "HMACSHA256="

// timestampLayout is the ISO 8601 UTC form DOKU writes and expects, without
// fractional seconds.
const timestampLayout = "2006-01-02T15:04:05Z"

// signed are the inputs of one non-SNAP signature. Body is nil for a request
// without one (GET), which has no Digest component.
type signed struct {
	ClientID  string
	RequestID string
	Timestamp string
	Target    string
	Body      []byte
}

// digest is base64(SHA-256(body)).
func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// components is the string DOKU signs: one "Name:value" line per component,
// joined with "\n" and no trailing newline.
func (s signed) components() string {
	var b strings.Builder
	b.WriteString("Client-Id:" + s.ClientID)
	b.WriteString("\nRequest-Id:" + s.RequestID)
	b.WriteString("\nRequest-Timestamp:" + s.Timestamp)
	b.WriteString("\nRequest-Target:" + s.Target)
	if s.Body != nil {
		b.WriteString("\nDigest:" + digest(s.Body))
	}
	return b.String()
}

// sign is the Signature header value for s under secret.
func (s signed) sign(secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(s.components()))
	return signaturePrefix + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// setHeaders signs a request to DOKU.
func (s signed) setHeaders(h http.Header, secret string) {
	h.Set(headerClientID, s.ClientID)
	h.Set(headerRequestID, s.RequestID)
	h.Set(headerRequestTimestamp, s.Timestamp)
	h.Set(headerSignature, s.sign(secret))
}

// verifyNotification checks a notification's headers against the client id
// and secret key of the connection. target is the path of the notification
// URL DOKU called.
func verifyNotification(h http.Header, body []byte, target, clientID, secret string) bool {
	got := h.Get(headerSignature)
	if got == "" || clientID == "" || secret == "" || h.Get(headerRequestID) == "" || h.Get(headerRequestTimestamp) == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(h.Get(headerClientID)), []byte(clientID)) != 1 {
		return false
	}
	if body == nil {
		body = []byte{}
	}
	want := signed{
		ClientID:  clientID,
		RequestID: h.Get(headerRequestID),
		Timestamp: h.Get(headerRequestTimestamp),
		Target:    target,
		Body:      body,
	}.sign(secret)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// timestamp formats t as a Request-Timestamp.
func timestamp(t time.Time) string { return t.UTC().Format(timestampLayout) }
