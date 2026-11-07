package term

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	"github.com/termwright/termwright/internal/session"
)

// telnetTLS wraps a Telnet connection in TLS (telnets, RFC 4217 style implicit
// TLS): the certificate is checked against the system's certificate
// authorities and the host name, unless the session accepts any certificate
// (self-signed certificates of appliances).
func telnetTLS(ctx context.Context, c net.Conn, s session.Session) (net.Conn, error) {
	tc := tls.Client(c, &tls.Config{ServerName: s.Hostname, InsecureSkipVerify: s.TLSAnyCert, MinVersion: tls.VersionTLS12}) //nolint:gosec // the user's choice, per session
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = c.Close()
		var ua x509.UnknownAuthorityError
		var hn x509.HostnameError
		var ci x509.CertificateInvalidError
		if errors.As(err, &ua) || errors.As(err, &hn) || errors.As(err, &ci) {
			return nil, fmt.Errorf("the TLS certificate of %s is not trusted (%v). If it is the device's own (self-signed) certificate, tick \"Accept any certificate\" in Session Options → Connection", s.Hostname, err)
		}
		return nil, fmt.Errorf("TLS with %s failed: %w", s.Hostname, err)
	}
	return tc, nil
}
