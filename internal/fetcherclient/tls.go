package fetcherclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func NewTLSConfig(caFile, certFile, keyFile, peerName string) (*tls.Config, error) {
	if peerName == "" {
		return nil, fmt.Errorf("TLS peer name is required")
	}
	pem, err := os.ReadFile(caFile) // #nosec G304 -- path is supplied by local configuration or a test fixture
	if err != nil {
		return nil, fmt.Errorf("read Fetcher CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("Fetcher CA contains no certificates") //nolint:staticcheck // Preserve the existing diagnostic text.
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load Fetcher certificate: %w", err)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, RootCAs: pool, ServerName: peerName}
	return config, nil
}
