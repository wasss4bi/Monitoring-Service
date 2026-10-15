package main

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerClosesSlowRequestBody(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newHTTPServer("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { b := make([]byte, 10); _, _ = r.Body.Read(b) }))
	go srv.Serve(ln)
	defer srv.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Write([]byte("POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var b [1]byte
	_, err = c.Read(b[:])
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("server retained slow request body beyond deadline")
	}
}

func TestServerClosesSlowHeaders(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newHTTPServer("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	go srv.Serve(ln)
	defer srv.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("POST / HTTP/1.1\r\nHost:")); err != nil {
		t.Fatal(err)
	}
	if err = c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	_, err = c.Read(b[:])
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("server retained incomplete headers beyond deadline")
	}
}
