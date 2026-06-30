//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"monitoring_service/internal/broker"
	"monitoring_service/internal/repository"
	"monitoring_service/internal/service"
)

// The broker and database are dedicated fixtures; no existing containers are stopped.
func TestOutboxRecoversAfterRabbitMQRestart(t *testing.T) {
	if os.Getenv("INTEGRATION_DATABASE_URL") == "" {
		t.Skip("INTEGRATION_DATABASE_URL is required")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required for the isolated RabbitMQ fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	db := monitoringTestDatabase(t)
	var databaseName string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(os.Getenv("INTEGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dsn.Path = "/" + databaseName
	query := dsn.Query()
	query.Set("application_name", "outbox-recovery-fixture")
	dsn.RawQuery = query.Encode()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	container := "monitoring-outbox-it-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	docker := func(args ...string) {
		t.Helper()
		output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() // #nosec G204 -- test launches a known executable with fixture-generated arguments
		if err != nil {
			t.Fatalf("docker %s: %v: %s", args[0], err, output)
		}
	}
	docker("run", "-d", "--name", container, "-p", fmt.Sprintf("127.0.0.1:%d:5672", port), "-e", "RABBITMQ_DEFAULT_USER=outbox_test", "-e", "RABBITMQ_DEFAULT_PASS=outbox_test", "rabbitmq:4-management")
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if output, err := exec.CommandContext(cleanup, "docker", "rm", "-f", "-v", container).CombinedOutput(); err != nil { // #nosec G204 -- test launches a known executable with fixture-generated arguments
			t.Errorf("remove isolated RabbitMQ fixture: %v: %s", err, output)
		}
	})
	address := fmt.Sprintf("amqp://outbox_test:outbox_test@127.0.0.1:%d/", port)
	waitForOutboxCondition(t, ctx, "isolated RabbitMQ startup", func() bool {
		conn, err := amqp.DialConfig(address, amqp.Config{Dial: amqp.DefaultDial(time.Second)})
		if err != nil {
			return false
		}
		return conn.Close() == nil
	})
	docker("stop", "--time", "2", container)

	monitor := createPriceMonitor(t, db, "https://outbox-recovery.example.com/product", true)
	executionRepo := repository.NewMonitoringExecutionRepository(db)
	count, err := executionRepo.ScheduleDue(ctx, 10*time.Minute, 10)
	if err != nil || count != 1 {
		t.Fatalf("schedule while broker is down: count=%d err=%v", count, err)
	}
	var fetchID, eventID string
	if err := db.QueryRowContext(ctx, `SELECT payload->>'fetch_id',event_id::text FROM outbox_events WHERE aggregate_type='monitor' AND aggregate_id=$1`, monitor.ID).Scan(&fetchID, &eventID); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(t.TempDir(), "outbox-publisher")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/outbox_publisher") // #nosec G204 -- test launches a known executable with fixture-generated arguments
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build outbox publisher: %v: %s", err, output)
	}
	logFile, err := os.Create(filepath.Join(t.TempDir(), "publisher.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			t.Errorf("close logFile: %v", err)
		}
	}()
	process := exec.CommandContext(ctx, binary) // #nosec G204 -- test launches a known executable with fixture-generated arguments
	process.Dir = t.TempDir()
	process.Env = append(os.Environ(), "DATABASE_URL="+dsn.String(), "RABBITMQ_URL="+address, "OUTBOX_POLL_INTERVAL=100ms", "OUTBOX_LEASE_DURATION=5s", "METRICS_PORT=")
	process.Stdout, process.Stderr = logFile, logFile
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	waitForOutboxCondition(t, ctx, "publisher database connection", func() bool {
		var n int
		return db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='outbox-recovery-fixture'`).Scan(&n) == nil && n > 0
	})
	var unpublished bool
	if err := db.QueryRowContext(ctx, `SELECT published_at IS NULL FROM outbox_events WHERE event_id=$1`, eventID).Scan(&unpublished); err != nil || !unpublished {
		t.Fatalf("event must stay pending while broker is down: pending=%t err=%v", unpublished, err)
	}

	docker("start", container)
	waitForOutboxCondition(t, ctx, "outbox publication after RabbitMQ restart", func() bool {
		var published bool
		return db.QueryRowContext(ctx, `SELECT published_at IS NOT NULL FROM outbox_events WHERE event_id=$1`, eventID).Scan(&published) == nil && published
	})
	conn, err := amqp.DialConfig(address, amqp.Config{Dial: amqp.DefaultDial(3 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	}()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ch.Close(); err != nil {
			t.Errorf("close ch: %v", err)
		}
	}()
	delivery, ok, err := ch.Get(broker.EndpointFetchJSONQueue, false)
	if err != nil || !ok || delivery.MessageId != eventID {
		t.Fatalf("expected published event %s: received=%t id=%s err=%v", eventID, ok, delivery.MessageId, err)
	}
	var payload struct {
		FetchID string `json:"fetch_id"`
	}
	if err := json.Unmarshal(delivery.Body, &payload); err != nil || payload.FetchID != fetchID {
		t.Fatalf("invalid published fetch payload: err=%v", err)
	}
	execution := service.NewMonitoringExecutionService(executionRepo, nil, 10*time.Minute, time.Minute, nil, nil).WithFetcher(fixturePriceBody(t, `{"woocommerce_product_object":{"regular_price":100}}`))
	for i := 0; i < 2; i++ {
		if err := execution.ProcessFetch(ctx, fetchID, eventID); err != nil {
			t.Fatalf("process/replay published event: %v", err)
		}
	}
	if err := delivery.Ack(false); err != nil {
		t.Fatal(err)
	}
	var evaluations, inbox int
	var status string
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM monitor_fetch_evaluations WHERE fetch_id=$1),(SELECT count(*) FROM processed_messages WHERE consumer_name=$2 AND message_id=$3),(SELECT status FROM monitors WHERE id=$4)`, fetchID, repository.EndpointFetchConsumer, eventID, monitor.ID).Scan(&evaluations, &inbox, &status); err != nil {
		t.Fatal(err)
	}
	if evaluations != 1 || inbox != 1 || status != "success" {
		t.Fatalf("unexpected effects after replay: evaluations=%d inbox=%d status=%s", evaluations, inbox, status)
	}
}

func waitForOutboxCondition(t *testing.T, ctx context.Context, description string, check func() bool) {
	t.Helper()
	for {
		if check() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", description, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
