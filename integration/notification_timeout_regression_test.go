//go:build integration

package integration

import (
	"github.com/google/uuid"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/sender"
	"monitoring_service/internal/service"
	"net"
	"testing"
	"time"
)

func TestNotificationSMTPTimeoutReleasesDelivery(t *testing.T) {
	db := monitoringTestDatabase(t)
	m := createPriceMonitor(t, db, "https://timeout.example.invalid/price", true)
	var id int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO notifications(monitor_id,user_contact_id,event_type,message) SELECT $1,id,'monitor.succeeded','test' FROM user_contacts WHERE user_id=$2 RETURNING id`, m.ID, m.UserID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := repository.NewMonitoringExecutionRepository(db)
	execution := service.NewMonitoringExecutionService(r, nil, time.Minute, time.Minute, sender.NewEmailSender(host, port, "", "", "from@example.invalid"), nil)
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- execution.ProcessNotification(t.Context(), id, uuid.NewString()) }()
	select {
	case c := <-accepted:
		defer c.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP connection not accepted")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("delivery exceeded 10-second budget")
	}
	if elapsed := time.Since(started); elapsed < 9*time.Second || elapsed > 12*time.Second {
		t.Fatalf("unexpected duration %s", elapsed)
	}
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM notifications WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status=%s, want retry", status)
	}
}
