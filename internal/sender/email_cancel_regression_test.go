package sender

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestEmailCancellationInterruptsSMTPGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close SMTP listener: %v", err)
		}
	}()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- NewEmailSender(host, port, "", "", "from@example.invalid").Send(ctx, "to@example.invalid", "test", "test")
	}()
	var conn net.Conn
	select {
	case conn = <-accepted:
		defer func() {
			if err := conn.Close(); err != nil {
				t.Errorf("close SMTP connection: %v", err)
			}
		}()
	case <-time.After(time.Second):
		t.Fatal("no SMTP connection")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled send succeeded")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("SMTP read did not stop after cancellation")
	}
}
