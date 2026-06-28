package fetcherclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func TestMTLSRequiresTrustedMonitoringIdentity(t *testing.T) {
	root := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal(err)
	}
	issue := func(name string, usage x509.ExtKeyUsage) (string, string) {
		t.Helper()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: serial, DNSNames: []string{name}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		certPath, keyPath := filepath.Join(root, name+".pem"), filepath.Join(root, name+".key")
		if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			t.Fatal(err)
		}
		return certPath, keyPath
	}
	serverCert, serverKey := issue("fetcher", x509.ExtKeyUsageServerAuth)
	clientCert, clientKey := issue("monitoring", x509.ExtKeyUsageClientAuth)
	serverTLS, err := serverFixtureTLS(caPath, serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	readiness := health.NewServer()
	readiness.SetServingStatus("direct", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, readiness)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-serveDone; err != nil {
			t.Errorf("serve gRPC: %v", err)
		}
	}()
	clientTLS, err := NewTLSConfig(caPath, clientCert, clientKey, "fetcher")
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(listener.Addr().String(), clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if !client.Healthy(ctx, "direct") {
		t.Fatal("authorized client rejected")
	}
	// A trusted CA signature does not authorize an unrelated client identity.
	otherCert, otherKey := issue("other", x509.ExtKeyUsageClientAuth)
	otherTLS, err := NewTLSConfig(caPath, otherCert, otherKey, "fetcher")
	if err != nil {
		t.Fatal(err)
	}
	other, err := New(listener.Addr().String(), otherTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Errorf("close other: %v", err)
		}
	}()
	denied, done := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer done()
	if other.Healthy(denied, "direct") {
		t.Fatal("wrong client identity accepted")
	}
	noCert := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: clientTLS.RootCAs, ServerName: "fetcher"}
	anonymous, err := New(listener.Addr().String(), noCert)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := anonymous.Close(); err != nil {
			t.Errorf("close anonymous: %v", err)
		}
	}()
	denied2, done2 := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer done2()
	if anonymous.Healthy(denied2, "direct") {
		t.Fatal("missing client certificate accepted")
	}
}

// The fake server enforces the external mTLS contract; Monitoring-Service only builds client TLS.
func serverFixtureTLS(caFile, certFile, keyFile string) (*tls.Config, error) {
	pem, err := os.ReadFile(caFile) // #nosec G304 -- path is supplied by local configuration or a test fixture
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, os.ErrInvalid
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, VerifyConnection: func(state tls.ConnectionState) error { return state.PeerCertificates[0].VerifyHostname("monitoring") }}, nil
}
