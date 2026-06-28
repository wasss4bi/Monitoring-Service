package sender

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestVerificationEmailRequiresStartTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close SMTP listener: %v", err)
		}
	}()
	done := make(chan string, 1)
	finished := make(chan struct{})
	t.Cleanup(func() { <-finished })
	go func() {
		defer close(finished)
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			done <- err.Error()
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Errorf("close SMTP connection: %v", err)
			}
		}()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := fmt.Fprint(conn, "220 localhost ESMTP\r\n"); err != nil {
			t.Errorf("write SMTP greeting: %v", err)
			return
		}
		r := bufio.NewReader(conn)
		line, _ := r.ReadString('\n')
		if strings.HasPrefix(line, "EHLO") {
			if _, err := fmt.Fprint(conn, "250 localhost\r\n"); err != nil {
				t.Errorf("write SMTP response: %v", err)
				return
			}
		}
		next, _ := r.ReadString('\n')
		done <- next
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	sender := NewVerificationEmailSender(host, port, "", "", "noreply@example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sender.Send(ctx, "target@example.com", "verify", "123456"); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("unexpected error: %v", err)
	}
	if line := <-done; strings.HasPrefix(line, "MAIL") {
		t.Fatal("sent mail before checking TLS")
	}
	<-done
}
