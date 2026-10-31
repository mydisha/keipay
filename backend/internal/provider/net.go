package provider

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
)

// NotSent reports whether a transport error happened before the request
// could reach the gateway: name resolution, connecting, or the TLS
// handshake. Such a request may be retried without risking a duplicate.
func NotSent(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	var (
		recordErr  tls.RecordHeaderError
		verifyErr  *tls.CertificateVerificationError
		authErr    x509.UnknownAuthorityError
		hostErr    x509.HostnameError
		invalidErr x509.CertificateInvalidError
	)
	return errors.As(err, &recordErr) || errors.As(err, &verifyErr) || errors.As(err, &authErr) ||
		errors.As(err, &hostErr) || errors.As(err, &invalidErr)
}
