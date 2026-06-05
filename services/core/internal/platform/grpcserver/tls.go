package grpcserver

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc/credentials"
)

// loadTransportCredentials returns TLS credentials, or (nil, nil) when TLS is
// not configured (development plain HTTP/2).
//
// TLS needs both cert and key; only one set is a misconfiguration that fails
// startup rather than silently downgrading. Adding a client CA upgrades to
// mTLS: the server requires and verifies a client certificate, restricting
// callers to services holding certs from the internal CA (zero-trust at the
// transport layer).
func loadTransportCredentials(cfg Config) (credentials.TransportCredentials, error) {
	if cfg.TLSCertFile == "" && cfg.TLSKeyFile == "" {
		return nil, nil
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, fmt.Errorf("grpcserver: TLS requires both cert and key files")
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("grpcserver: load tls keypair: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		// TLS 1.3 minimum: modern suites only, no protocol downgrade.
		MinVersion: tls.VersionTLS13,
	}

	if cfg.TLSClientCAFile != "" {
		// Operator-supplied startup path, normalized before the read.
		caBytes, err := os.ReadFile(filepath.Clean(cfg.TLSClientCAFile))
		if err != nil {
			return nil, fmt.Errorf("grpcserver: read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("grpcserver: client CA file contained no valid certificates")
		}
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = pool
	}

	return credentials.NewTLS(tlsConfig), nil
}
