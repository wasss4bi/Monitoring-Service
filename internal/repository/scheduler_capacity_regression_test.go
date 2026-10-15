//go:build integration

package repository

import (
	"sync"
	"testing"
	"time"
)

func TestBrowserBacklogDoesNotBlockDirect(t *testing.T) {
	db := monitorSessionDB(t)
	direct, _ := monitorSessionPair(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET status='stopped' WHERE id<>$1`, direct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `WITH e AS (INSERT INTO monitored_endpoints(canonical_url,type,target_hostname_id) SELECT 'https://sessions.example.com/html','html',target_hostname_id FROM monitored_endpoints WHERE id=$1 RETURNING id) INSERT INTO monitors(user_id,endpoint_id,name,path_type,rules_count_to_finish,page_url) SELECT $2,id,'browser','selector',1,'https://sessions.example.com/html' FROM e,generate_series(1,100)`, direct.EndpointID, direct.UserID); err != nil {
		t.Fatal(err)
	}
	r := NewMonitoringExecutionRepository(db)
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET next_check_at=NOW()+INTERVAL '1 hour' WHERE id=$1`, direct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var browserCount int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM endpoint_fetches`).Scan(&browserCount); err != nil {
		t.Fatal(err)
	}
	if browserCount != 50 {
		t.Fatalf("browser scheduled=%d; want 50", browserCount)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE monitors SET next_check_at=NOW()-INTERVAL '1 second' WHERE id=$1`, direct.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ScheduleDue(t.Context(), time.Minute, 100); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM endpoint_fetches WHERE monitor_id=$1`, direct.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("direct scheduled %d times despite available capacity", count)
	}
}

func TestConcurrentSchedulersRespectSharedCapacity(t *testing.T) {
	db := monitorSessionDB(t)
	direct, _ := monitorSessionPair(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO monitors(user_id,endpoint_id,name,path_type,rules_count_to_finish,page_url) SELECT $1,$2,'concurrent','json',1,'https://sessions.example.com/product' FROM generate_series(1,100)`, direct.UserID, direct.EndpointID); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := NewMonitoringExecutionRepository(db).ScheduleDue(t.Context(), time.Minute, 100)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM endpoint_fetches`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > 50 {
		t.Fatalf("concurrent direct jobs=%d; maximum 50", count)
	}
}
