package fetcherclient

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func TestClientDoesNotWaitForDNSServiceConfig(t *testing.T) {
	// Docker resolves service addresses but may stall TXT lookups. This client
	// uses the application's explicit configuration and needs no DNS policy.
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var txtQueries atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			n, address, err := dns.ReadFrom(buffer)
			if err != nil {
				return
			}
			var request dnsmessage.Message
			if err := request.Unpack(buffer[:n]); err != nil || len(request.Questions) != 1 {
				continue
			}
			question := request.Questions[0]
			if question.Type == dnsmessage.TypeTXT {
				txtQueries.Add(1)
				continue // Simulate the observed hanging _grpc_config lookup.
			}
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: request.ID, Response: true, RecursionAvailable: true}, Questions: request.Questions}
			if question.Type == dnsmessage.TypeA {
				response.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}}}
			}
			packed, err := response.Pack()
			if err == nil {
				_, _ = dns.WriteTo(packed, address)
			}
		}
	}()
	t.Cleanup(func() {
		if err := dns.Close(); err != nil {
			t.Errorf("close DNS listener: %v", err)
		}
		<-done
	})

	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certificateServer.TLS.Certificates[0]
	leaf := certificateServer.Certificate()
	certificateServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}})))
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
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(leaf.DNSNames[0], ".")
	client, err := New("dns://"+dns.LocalAddr().String()+"/fixture.test:"+port, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, ServerName: name})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close gRPC client: %v", err)
		}
	}()
	if !client.Healthy(t.Context(), "direct") {
		t.Fatalf("health check stalled on DNS; TXT queries=%d", txtQueries.Load())
	}
	if count := txtQueries.Load(); count != 0 {
		t.Fatalf("unexpected DNS service configuration lookup: %d", count)
	}
}
