package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"monitoring_service/internal/domain"
)

const (
	notificationEvent = "notification.deliver"
)

func endpointFetchEvent(endpointType string) (string, error) {
	switch endpointType {
	case domain.EndpointTypeJSON:
		return "endpoint.fetch.json", nil
	case domain.EndpointTypeHTML:
		return "endpoint.fetch.html", nil
	default:
		return "", fmt.Errorf("unsupported monitored endpoint type %q", endpointType)
	}
}

type MonitoringExecutionRepository struct {
	db               *sql.DB
	dependencyGate   bool
	fetcherHostnames []string
}

func (r *MonitoringExecutionRepository) WithDependencyGate(enabled bool, hostnames ...string) *MonitoringExecutionRepository {
	r.dependencyGate = enabled
	r.fetcherHostnames = hostnames
	return r
}

const (
	EndpointFetchConsumer = "endpoint-fetch-worker"
	NotificationConsumer  = "notification-worker"
)

func NewMonitoringExecutionRepository(db *sql.DB) *MonitoringExecutionRepository {
	return &MonitoringExecutionRepository{db: db}
}

func (r *MonitoringExecutionRepository) IsProcessed(ctx context.Context, consumerName, messageID string) (bool, error) {
	return NewProcessedMessageRepository().IsProcessed(ctx, r.db, consumerName, messageID)
}

// ScheduleDue creates an immutable fetch and its outbox event in one transaction.
// Each monitor is eligible only while its owner has an active verified recipient.
func (r *MonitoringExecutionRepository) ScheduleDue(ctx context.Context, interval time.Duration, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Rank only a bounded candidate slice per configured hostname. Outstanding
	// work is included in fairness, so a newly due hostname can take the next slot.
	type dueEndpoint struct {
		id           int64
		endpointID   int64
		endpointType string
	}
	var endpoints []dueEndpoint
	for _, browser := range []bool{false, true} {
		remaining := limit - len(endpoints)
		if remaining <= 0 {
			break
		}
		capability := "direct"
		if browser {
			capability = "browser"
		}
		var locked string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM scheduler_capacity_locks WHERE name=$1 FOR UPDATE`, capability).Scan(&locked); err != nil {
			return 0, fmt.Errorf("lock scheduler capacity: %w", err)
		}
		rows, err := tx.QueryContext(ctx, `WITH outstanding AS (
 SELECT e.target_hostname_id,count(*) AS jobs FROM endpoint_fetches f
 JOIN monitors owner ON owner.id=f.monitor_id JOIN monitored_endpoints e ON e.id=owner.endpoint_id JOIN target_hostnames ot ON ot.id=e.target_hostname_id
 WHERE f.status IN ('pending','fetching') AND owner.status='monitoring' AND owner.deleted_at IS NULL
 AND owner.generation=f.generation AND (e.type<>'json' OR e.browser_page_url IS NOT NULL OR e.json_source IS NOT NULL OR ot.bootstrap_url IS NOT NULL)=$4 GROUP BY e.target_hostname_id
 ) SELECT m.id,c.type,c.endpoint_id FROM target_hostnames t
 LEFT JOIN outstanding o ON o.target_hostname_id=t.id
 JOIN LATERAL (
  SELECT candidate.*,row_number() OVER(ORDER BY candidate.next_check_at NULLS FIRST,candidate.id) AS position
  FROM (SELECT selected.id,selected.next_check_at,e.id AS endpoint_id,e.type FROM monitors selected
   JOIN monitored_endpoints e ON e.id=selected.endpoint_id
   WHERE e.target_hostname_id=t.id AND selected.check_status IN ('idle','retry_wait')
   AND (e.type<>'json' OR e.browser_page_url IS NOT NULL OR e.json_source IS NOT NULL OR t.bootstrap_url IS NOT NULL)=$4
   AND selected.status='monitoring' AND selected.deleted_at IS NULL
   AND (selected.next_check_at IS NULL OR selected.next_check_at<=NOW())
   AND (NOT ($2 AND (COALESCE(cardinality($3::text[]),0)=0 OR t.hostname=ANY($3::text[])))
        OR NOT EXISTS(SELECT 1 FROM service_dependencies WHERE name='direct' AND status='down'))
   AND (NOT ($2 AND (COALESCE(cardinality($3::text[]),0)=0 OR t.hostname=ANY($3::text[])))
        OR (e.type='json' AND e.browser_page_url IS NULL AND e.json_source IS NULL AND t.bootstrap_url IS NULL)
        OR NOT EXISTS(SELECT 1 FROM service_dependencies WHERE name='browser' AND status='down'))
   AND EXISTS(SELECT 1 FROM user_contacts uc WHERE uc.user_id=selected.user_id AND uc.is_active AND uc.is_verified)
   ORDER BY selected.next_check_at NULLS FIRST,selected.id LIMIT $1
  ) candidate
 ) c ON true JOIN monitors m ON m.id=c.id
 WHERE t.is_active AND m.status='monitoring' AND m.deleted_at IS NULL AND m.check_status IN ('idle','retry_wait')
 ORDER BY COALESCE(o.jobs,0)+c.position,c.next_check_at NULLS FIRST,m.id
 FOR UPDATE OF m SKIP LOCKED LIMIT LEAST($1,GREATEST(0,50-(SELECT COALESCE(sum(jobs),0) FROM outstanding)))`, remaining, r.dependencyGate, pq.Array(r.fetcherHostnames), browser)
		if err != nil {
			return 0, fmt.Errorf("select due endpoints: %w", err)
		}
		for rows.Next() {
			var endpoint dueEndpoint
			if err := rows.Scan(&endpoint.id, &endpoint.endpointType, &endpoint.endpointID); err != nil {
				rows.Close()
				return 0, err
			}
			endpoints = append(endpoints, endpoint)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		rows.Close()
	}
	for _, endpoint := range endpoints {
		eventType, err := endpointFetchEvent(endpoint.endpointType)
		if err != nil {
			return 0, err
		}
		var generation int64
		if err := tx.QueryRowContext(ctx, `UPDATE monitors SET check_status='scheduled', generation=generation+1, next_check_at=NOW()+($2 * INTERVAL '1 microsecond'), updated_at=NOW() WHERE id=$1 RETURNING generation`, endpoint.id, interval.Microseconds()).Scan(&generation); err != nil {
			return 0, fmt.Errorf("schedule endpoint: %w", err)
		}
		var fetchID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO endpoint_fetches(endpoint_id,generation,scheduled_at,monitor_id,next_dispatch_at) VALUES($1,$2,NOW(),$3,NOW()+INTERVAL '30 seconds') RETURNING fetch_id::text`, endpoint.endpointID, generation, endpoint.id).Scan(&fetchID); err != nil {
			return 0, fmt.Errorf("create endpoint fetch: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{"fetch_id": fetchID})
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'monitor',$2,$3,NOW())`, eventType, endpoint.id, payload); err != nil {
			return 0, fmt.Errorf("create fetch outbox event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(endpoints), nil
}

func (r *MonitoringExecutionRepository) ClaimFetch(ctx context.Context, fetchID, token string, lease time.Duration) (domain.FetchJob, error) {
	var job domain.FetchJob
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	// Lock the subscriber before its job, matching credential replacement.
	if err = tx.QueryRowContext(ctx, `SELECT m.id FROM monitors m JOIN endpoint_fetches f ON f.monitor_id=m.id
 WHERE f.fetch_id=$1::uuid AND m.generation=f.generation AND m.status='monitoring' AND m.deleted_at IS NULL
 AND m.check_status='scheduled' AND f.available_at<=NOW() FOR UPDATE OF m`, fetchID).Scan(&job.MonitorID); err != nil {
		return job, err
	}
	var rawHeaders []byte
	var rawJSONSource []byte
	err = tx.QueryRowContext(ctx, `
      WITH claimed AS (
		UPDATE endpoint_fetches f SET status='fetching', started_at=COALESCE(started_at,NOW()), lease_token=$2::uuid, lease_until=NOW()+($3*INTERVAL '1 microsecond')
		FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id
        WHERE f.fetch_id=$1::uuid AND f.endpoint_id=e.id AND f.monitor_id=m.id AND f.generation=m.generation
		  AND t.is_active AND m.status='monitoring' AND m.deleted_at IS NULL
          AND (f.status='pending' OR (f.status='fetching' AND f.lease_until<NOW()))
		RETURNING f.fetch_id::text,f.endpoint_id,e.canonical_url,e.type,'{}'::jsonb /* caller headers disabled */,m.attempts_count,m.page_url,COALESCE(e.browser_page_url,''),COALESCE(e.json_source,'null'::jsonb),
		          t.id,t.hostname,t.limit_per_minute,
		          COALESCE(t.bootstrap_url,''),COALESCE(m.session_ciphertext,''),m.session_generation,m.id,m.generation,m.session_mode,m.profile_generation
	  ) SELECT * FROM claimed`, fetchID, token, lease.Microseconds()).Scan(
		&job.FetchID, &job.EndpointID, &job.CanonicalURL, &job.EndpointType, &rawHeaders, &job.AttemptsCount,
		&job.PageURL, &job.BrowserPageURL, &rawJSONSource,
		&job.TargetHostnameID, &job.TargetHostname, &job.TargetLimitPerMin,
		&job.BootstrapURL, &job.SessionCiphertext, &job.SessionGeneration, &job.MonitorID, &job.Generation, &job.SessionMode, &job.ProfileGeneration,
	)
	if err != nil {
		return domain.FetchJob{}, err
	}
	if err = json.Unmarshal(rawHeaders, &job.Headers); err != nil {
		return domain.FetchJob{}, fmt.Errorf("decode endpoint headers: %w", err)
	}
	if err = json.Unmarshal(rawJSONSource, &job.JSONSource); err != nil {
		return domain.FetchJob{}, fmt.Errorf("decode endpoint JSON source: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE monitors SET check_status='fetching',updated_at=NOW() WHERE id=$1 AND check_status='scheduled'`, job.MonitorID)
	if err != nil {
		return domain.FetchJob{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.FetchJob{}, err
	}
	if changed != 1 {
		return domain.FetchJob{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return domain.FetchJob{}, err
	}
	return job, nil
}

// RecoverExpiredFetches requeues the same fetch after a worker crash or lost delivery.
func (r *MonitoringExecutionRepository) RecoverExpiredFetches(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT f.fetch_id::text,f.monitor_id,f.generation,m.generation,e.type,(m.status='monitoring' AND m.deleted_at IS NULL) FROM endpoint_fetches f JOIN monitors m ON m.id=f.monitor_id JOIN monitored_endpoints e ON e.id=f.endpoint_id WHERE f.status='fetching' AND f.lease_until<NOW() ORDER BY f.lease_until,f.fetch_id FOR UPDATE OF m SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type expired struct {
		id                                             string
		endpointID, fetchGeneration, currentGeneration int64
		endpointType                                   string
		active                                         bool
	}
	var jobs []expired
	for rows.Next() {
		var job expired
		if err := rows.Scan(&job.id, &job.endpointID, &job.fetchGeneration, &job.currentGeneration, &job.endpointType, &job.active); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, job := range jobs {
		if job.fetchGeneration != job.currentGeneration || !job.active {
			if _, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='failed',finished_at=NOW(),lease_token=NULL,lease_until=NULL,last_error='superseded by a newer generation' WHERE fetch_id=$1::uuid`, job.id); err != nil {
				return 0, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='pending',lease_token=NULL,lease_until=NULL,last_error='worker lease expired' WHERE fetch_id=$1::uuid`, job.id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE monitors SET check_status='scheduled',updated_at=NOW() WHERE id=$1 AND generation=$2`, job.endpointID, job.currentGeneration); err != nil {
			return 0, err
		}
		payload, err := json.Marshal(map[string]string{"fetch_id": job.id})
		if err != nil {
			return 0, err
		}
		eventType, err := endpointFetchEvent(job.endpointType)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'monitor',$2,$3,NOW())`, eventType, job.endpointID, payload); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(jobs), nil
}

func (r *MonitoringExecutionRepository) CompleteFetchSuccess(ctx context.Context, fetchID, token, messageID string, status int, body []byte, nextCheck time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	owner, err := lockOwnedFetch(ctx, tx, fetchID, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='succeeded',finished_at=NOW(),lease_token=NULL,lease_until=NULL,
 last_error=NULL,response_status=$2,response_json=NULL WHERE fetch_id=$1::uuid`, fetchID, status); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE monitors SET check_status='idle',last_attempt_at=NOW(),last_success_at=NOW(),
 attempts_count=0,consecutive_errors=0,retry_started_at=NULL,
 next_check_at=NOW()+($2*INTERVAL '1 microsecond'),updated_at=NOW() WHERE id=$1`, owner, nextCheck.Microseconds()); err != nil {
		return err
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, EndpointFetchConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MonitoringExecutionRepository) AcquireSessionRefresh(ctx context.Context, id, generation int64, token string, lease time.Duration) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE monitors SET session_status='refreshing',refresh_lease_token=$3::uuid,
 refresh_lease_until=NOW()+($4*INTERVAL '1 microsecond'),updated_at=NOW()
 WHERE id=$1 AND session_generation=$2 AND session_mode='managed' AND status='monitoring' AND deleted_at IS NULL
 AND (session_status IN ('missing','ready') OR (session_status='refreshing' AND refresh_lease_until<NOW()))`, id, generation, token, lease.Microseconds())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *MonitoringExecutionRepository) CompleteSessionRefresh(ctx context.Context, targetID int64, token, ciphertext string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE monitors
		SET session_ciphertext=$3, session_status='ready', session_generation=session_generation+1,
		    refresh_lease_token=NULL, refresh_lease_until=NULL, session_updated_at=NOW(),
		    last_session_error=NULL, updated_at=NOW()
		WHERE id=$1 AND refresh_lease_token=$2::uuid AND session_status='refreshing'
	`, targetID, token, ciphertext)
	if err != nil {
		return fmt.Errorf("complete target session refresh: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// Browser admission failure must not turn a valid session into a blocked target.
func (r *MonitoringExecutionRepository) ReleaseSessionRefresh(ctx context.Context, targetID int64, token string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE monitors
		SET session_status=CASE WHEN session_ciphertext IS NULL THEN 'missing' ELSE 'ready' END,
		    refresh_lease_token=NULL, refresh_lease_until=NULL, updated_at=NOW()
		WHERE id=$1 AND refresh_lease_token=$2::uuid AND session_status='refreshing'
	`, targetID, token)
	if err != nil {
		return fmt.Errorf("release target session refresh: %w", err)
	}
	return nil
}

func (r *MonitoringExecutionRepository) SaveMonitorSession(ctx context.Context, targetID, expectedGeneration int64, ciphertext string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE monitors
		SET session_ciphertext=$3,session_status='ready', session_updated_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND session_generation=$2 AND session_status IN ('ready','missing') AND deleted_at IS NULL
	`, targetID, expectedGeneration, ciphertext)
	if err != nil {
		return fmt.Errorf("save target session: %w", err)
	}
	return nil
}

// RejectMonitorSession does not block the hostname or erase user credentials.
func (r *MonitoringExecutionRepository) RejectMonitorSession(ctx context.Context, id, generation int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE monitors SET session_status='blocked',
 last_session_error='session rejected',refresh_lease_token=NULL,refresh_lease_until=NULL,updated_at=NOW()
 WHERE id=$1 AND session_generation=$2`, id, generation)
	return err
}

func (r *MonitoringExecutionRepository) CompleteFetchFailure(ctx context.Context, fetchID, token, messageID string, status *int, reason string, retryAfter time.Duration, retry bool, terminalEndpointStatus string) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	owner, err := lockOwnedFetch(ctx, tx, fetchID, token)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='failed',finished_at=NOW(),lease_token=NULL,lease_until=NULL,
 response_status=$2,last_error=$3 WHERE fetch_id=$1::uuid`, fetchID, status, reason); err != nil {
		return 0, err
	}
	state := "error"
	if retry {
		state = "retry_wait"
	} else if terminalEndpointStatus == "access_denied" {
		state = "access_denied"
	}
	var attempts int64
	err = tx.QueryRowContext(ctx, `UPDATE monitors SET check_status=$2,last_attempt_at=NOW(),last_checked_at=NOW(),attempts_count=attempts_count+1,
 consecutive_errors=consecutive_errors+1,retry_started_at=COALESCE(retry_started_at,NOW()),last_error=$3,
 next_check_at=CASE WHEN $4 THEN NOW()+($5*INTERVAL '1 microsecond') ELSE NULL END,updated_at=NOW()
 WHERE id=$1 RETURNING attempts_count`, owner, state, reason, retry, retryAfter.Microseconds()).Scan(&attempts)
	if err != nil {
		return 0, err
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, EndpointFetchConsumer, messageID); err != nil {
		return 0, err
	}
	return attempts, tx.Commit()
}

// DeferFetch releases a claimed fetch without consuming an endpoint retry.
// The delayed outbox event reuses this fetch when the domain window opens.

func (r *MonitoringExecutionRepository) DeferFetch(ctx context.Context, fetchID, token, messageID string, delay time.Duration) error {
	return r.deferFetch(ctx, fetchID, token, messageID, delay, "")
}

func (r *MonitoringExecutionRepository) deferFetch(ctx context.Context, fetchID, token, messageID string, delay time.Duration, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	owner, err := lockOwnedFetch(ctx, tx, fetchID, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var kind string
	if err = tx.QueryRowContext(ctx, `SELECT e.type FROM monitors m JOIN monitored_endpoints e ON e.id=m.endpoint_id WHERE m.id=$1`, owner).Scan(&kind); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='pending',lease_token=NULL,lease_until=NULL,
 defer_reason=NULLIF($3,''),available_at=NOW()+($2*INTERVAL '1 second'),next_dispatch_at=NOW()+(($2+30)*INTERVAL '1 second') WHERE fetch_id=$1::uuid`, fetchID, delay.Seconds(), reason); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE monitors SET check_status='scheduled',next_check_at=NOW()+($2*INTERVAL '1 microsecond'),updated_at=NOW() WHERE id=$1`, owner, delay.Microseconds()); err != nil {
		return err
	}
	event, err := endpointFetchEvent(kind)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"fetch_id": fetchID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at)
 VALUES($1,'monitor',$2,$3,NOW()+($4*INTERVAL '1 microsecond'))`, event, owner, payload, delay.Microseconds()); err != nil {
		return err
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, EndpointFetchConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

// SwitchEndpointType atomically retires selectors for the former content type and
// advances the endpoint generation, so an in-flight job cannot run under the new type.
func (r *MonitoringExecutionRepository) SwitchEndpointType(ctx context.Context, fetchID, token, messageID, endpointType, reason string, nextCheck time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var endpointID int64
	if err = tx.QueryRowContext(ctx, `SELECT e.id FROM endpoint_fetches f JOIN monitored_endpoints e ON e.id=f.endpoint_id WHERE f.fetch_id=$1::uuid AND f.lease_token=$2::uuid AND f.status='fetching' AND f.generation=e.generation FOR UPDATE OF f,e`, fetchID, token).Scan(&endpointID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id,name FROM monitors WHERE endpoint_id=$1 AND status='monitoring' AND deleted_at IS NULL ORDER BY id FOR UPDATE`, endpointID)
	if err != nil {
		return err
	}
	type monitor struct {
		id, userID int64
		name       string
	}
	var monitors []monitor
	for rows.Next() {
		var item monitor
		if err := rows.Scan(&item.id, &item.userID, &item.name); err != nil {
			rows.Close()
			return err
		}
		monitors = append(monitors, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, monitor := range monitors {
		if _, err := tx.ExecContext(ctx, `INSERT INTO monitor_fetch_evaluations(fetch_id,monitor_id) VALUES($1::uuid,$2) ON CONFLICT DO NOTHING`, fetchID, monitor.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE monitors SET status='error',last_checked_at=NOW(),last_error=$2,completed_at=NOW(),updated_at=NOW() WHERE id=$1`, monitor.id, reason); err != nil {
			return err
		}
		contacts, err := tx.QueryContext(ctx, `SELECT id FROM user_contacts WHERE user_id=$1 AND is_active AND is_verified ORDER BY id`, monitor.userID)
		if err != nil {
			return err
		}
		for contacts.Next() {
			var contactID, notificationID int64
			if err := contacts.Scan(&contactID); err != nil {
				contacts.Close()
				return err
			}
			message := fmt.Sprintf("Monitor %q stopped: %s", monitor.name, reason)
			err = tx.QueryRowContext(ctx, `INSERT INTO notifications(monitor_id,user_contact_id,event_type,message) VALUES($1,$2,'monitor.failed',$3) ON CONFLICT(monitor_id,event_type,user_contact_id) DO NOTHING RETURNING id`, monitor.id, contactID, message).Scan(&notificationID)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				contacts.Close()
				return err
			}
			payload, err := json.Marshal(map[string]int64{"notification_id": notificationID})
			if err != nil {
				contacts.Close()
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'notification',$2,$3,NOW())`, notificationEvent, notificationID, payload); err != nil {
				contacts.Close()
				return err
			}
		}
		if err := contacts.Close(); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='failed',finished_at=NOW(),lease_token=NULL,lease_until=NULL,last_error=$3 WHERE fetch_id=$1::uuid AND lease_token=$2::uuid`, fetchID, token, reason); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE monitored_endpoints SET type=$2,status='idle',generation=generation+1,next_check_at=NOW()+($3*INTERVAL '1 microsecond'),last_checked_at=NOW(),last_error=$4,lease_token=NULL,lease_until=NULL,updated_at=NOW() WHERE id=$1`, endpointID, endpointType, nextCheck.Microseconds(), reason); err != nil {
		return err
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, EndpointFetchConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

type EvaluationMonitor struct {
	ID                 int64
	UserID             int64
	PathType           string
	RulesCountToFinish int
	Rules              []domain.MonitorRule
}

func (r *MonitoringExecutionRepository) ActiveMonitors(ctx context.Context, endpointID int64) ([]EvaluationMonitor, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT m.id,m.user_id,m.path_type,m.rules_count_to_finish,r.id,r.json_path,r.comparison_operator,r.expected_value,r.source,r.number_format,r.description FROM monitors m JOIN monitor_rules r ON r.monitor_id=m.id WHERE m.id=$1 AND m.status='monitoring' AND m.deleted_at IS NULL ORDER BY m.id,r.id`, endpointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []EvaluationMonitor{}
	byID := map[int64]int{}
	for rows.Next() {
		var mID, uID, rID int64
		var pathType, p, o string
		var rulesCountToFinish int
		var v float64
		var rawSource []byte
		var format, description string
		if err := rows.Scan(&mID, &uID, &pathType, &rulesCountToFinish, &rID, &p, &o, &v, &rawSource, &format, &description); err != nil {
			return nil, err
		}
		idx, ok := byID[mID]
		if !ok {
			idx = len(result)
			byID[mID] = idx
			result = append(result, EvaluationMonitor{ID: mID, UserID: uID, PathType: pathType, RulesCountToFinish: rulesCountToFinish})
		}
		var source *domain.RuleSource
		if len(rawSource) > 0 {
			if err := json.Unmarshal(rawSource, &source); err != nil {
				return nil, err
			}
		}
		result[idx].Rules = append(result[idx].Rules, domain.MonitorRule{Description: description, Source: source, NumberFormat: format, ID: rID, MonitorID: mID, JSONPath: p, ComparisonOperator: o, ExpectedValue: v})
	}
	return result, rows.Err()
}

// RecordMonitorMismatch records a checked but unsatisfied rule without completing the monitor.
func (r *MonitoringExecutionRepository) RecordMonitorMismatch(ctx context.Context, fetchID, token string, monitorID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	recorded, err := recordEvaluation(ctx, tx, fetchID, token, monitorID)
	if err != nil {
		return err
	}
	if !recorded {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE monitors SET last_checked_at=NOW(),last_error=NULL,updated_at=NOW() WHERE id=$1 AND status='monitoring' AND deleted_at IS NULL`, monitorID); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishMonitor records an evaluation once, changes the terminal monitor state and queues one notification per currently eligible contact.
func (r *MonitoringExecutionRepository) FinishMonitor(ctx context.Context, fetchID, token string, monitorID int64, succeeded bool, message string, matchedRules []domain.MonitorRule) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := finishMonitorTx(ctx, tx, fetchID, token, monitorID, succeeded, message, matchedRules); err != nil {
		return err
	}
	return tx.Commit()
}

func finishMonitorTx(ctx context.Context, tx *sql.Tx, fetchID, token string, monitorID int64, succeeded bool, message string, matchedRules []domain.MonitorRule) error {
	recorded, err := recordEvaluation(ctx, tx, fetchID, token, monitorID)
	if err != nil {
		return err
	}
	if !recorded {
		return nil
	}
	status := "error"
	if succeeded {
		status = "success"
	}
	var userID int64
	var monitor domain.Monitor
	if err = tx.QueryRowContext(ctx, `UPDATE monitors m SET status=$2::varchar,last_checked_at=NOW(),last_error=CASE WHEN $2::varchar='error'::varchar THEN $3 ELSE NULL END,completed_at=NOW(),updated_at=NOW() FROM monitored_endpoints e WHERE m.id=$1 AND m.endpoint_id=e.id AND m.status='monitoring' AND m.deleted_at IS NULL RETURNING m.user_id,m.id,m.name,m.page_url,m.created_at,m.completed_at,e.canonical_url,e.type,m.rules_count_to_finish`, monitorID, status, message).Scan(&userID, &monitor.ID, &monitor.Name, &monitor.PageURL, &monitor.CreatedAt, &monitor.CompletedAt, &monitor.URL, &monitor.EndpointType, &monitor.RulesCountToFinish); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	event := "monitor.failed"
	if succeeded {
		event = "monitor.succeeded"
		if err := validateMatchedRules(monitor, matchedRules); err != nil {
			return err
		}
		message = fmt.Sprintf("Monitor %q triggered!", monitor.Name)
	} else {
		message = fmt.Sprintf("Monitor %q stopped: %s", monitor.Name, message)
	}
	rows, err := tx.QueryContext(ctx, `SELECT uc.id,lower(c.name) FROM user_contacts uc JOIN contacts c ON c.id=uc.contact_id WHERE uc.user_id=$1 AND uc.is_active AND uc.is_verified ORDER BY uc.id`, userID)
	if err != nil {
		return err
	}
	type recipient struct {
		id      int64
		channel string
	}
	var contacts []recipient
	for rows.Next() {
		var contact recipient
		if err := rows.Scan(&contact.id, &contact.channel); err != nil {
			rows.Close()
			return err
		}
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, contact := range contacts {
		body := message
		if succeeded && contact.channel == "telegram" {
			body = formatMonitorSuccess(monitor, matchedRules)
		}
		var notificationID int64
		err = tx.QueryRowContext(ctx, `INSERT INTO notifications(monitor_id,user_contact_id,event_type,message) VALUES($1,$2,$3,$4) ON CONFLICT(monitor_id,event_type,user_contact_id) DO NOTHING RETURNING id`, monitorID, contact.id, event, body).Scan(&notificationID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]int64{"notification_id": notificationID})
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'notification',$2,$3,NOW())`, notificationEvent, notificationID, payload); err != nil {
			return err
		}
	}
	return nil
}

func recordEvaluation(ctx context.Context, tx *sql.Tx, fetchID, token string, monitorID int64) (bool, error) {

	owner, err := lockOwnedFetch(ctx, tx, fetchID, token)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if owner != monitorID {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO monitor_fetch_evaluations(fetch_id,monitor_id) VALUES($1::uuid,$2) ON CONFLICT DO NOTHING`, fetchID, monitorID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func (r *MonitoringExecutionRepository) ClaimNotification(ctx context.Context, notificationID int64, lease time.Duration) (domain.NotificationJob, error) {
	var job domain.NotificationJob
	job.LeaseToken = uuid.NewString()
	err := r.db.QueryRowContext(ctx, `UPDATE notifications n SET status='sending',lease_token=$3::uuid,attempts_count=attempts_count+1,next_attempt_at=NOW()+($2*INTERVAL '1 microsecond') FROM user_contacts uc JOIN contacts c ON c.id=uc.contact_id WHERE n.id=$1 AND n.user_contact_id=uc.id AND (n.status='pending' OR (n.status='sending' AND n.next_attempt_at<NOW())) AND uc.is_active AND uc.is_verified RETURNING n.id,n.monitor_id,n.user_contact_id,n.event_type,n.message,n.status,n.attempts_count,n.last_error,n.next_attempt_at,n.created_at,n.sent_at,n.idempotency_key::text,c.name,uc.contact,uc.updated_at`, notificationID, lease.Microseconds(), job.LeaseToken).Scan(&job.Notification.ID, &job.Notification.MonitorID, &job.Notification.UserContactID, &job.Notification.EventType, &job.Notification.Message, &job.Notification.Status, &job.Notification.AttemptsCount, &job.Notification.LastError, &job.Notification.NextAttemptAt, &job.Notification.CreatedAt, &job.Notification.SentAt, &job.Notification.IdempotencyKey, &job.Channel, &job.Contact, &job.ContactUpdatedAt)
	if err != nil {
		return job, err
	}
	return job, nil
}

// RecoverExpiredNotifications republishes deliveries whose worker lease expired before an acknowledgement.
func (r *MonitoringExecutionRepository) RecoverExpiredNotifications(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,attempts_count,created_at FROM notifications WHERE status='sending' AND next_attempt_at<NOW() ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type expired struct {
		id        int64
		attempts  int
		createdAt time.Time
	}
	var jobs []expired
	for rows.Next() {
		var job expired
		if err := rows.Scan(&job.id, &job.attempts, &job.createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, job := range jobs {
		if job.attempts >= 6 || time.Since(job.createdAt) >= 10*time.Minute {
			if _, err := tx.ExecContext(ctx, `UPDATE notifications SET status='failed',last_error='delivery lease expired after retry window' WHERE id=$1`, job.id); err != nil {
				return 0, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE notifications SET status='pending',lease_token=NULL,next_attempt_at=NOW(),last_error='delivery lease expired' WHERE id=$1`, job.id); err != nil {
			return 0, err
		}
		payload, err := json.Marshal(map[string]int64{"notification_id": job.id})
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'notification',$2,$3,NOW())`, notificationEvent, job.id, payload); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(jobs), nil
}
func (r *MonitoringExecutionRepository) CompleteNotification(ctx context.Context, id int64, token, messageID, errText string, retryAfter time.Duration, retry bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status := "sent"
	if errText != "" {
		status = "failed"
		if retry {
			status = "pending"
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE notifications SET status=$2::varchar,lease_token=NULL,last_error=NULLIF($3,''),sent_at=CASE WHEN $2::varchar='sent'::varchar THEN NOW() ELSE sent_at END,next_attempt_at=CASE WHEN $4 THEN NOW()+($5*INTERVAL '1 microsecond') ELSE next_attempt_at END WHERE id=$1 AND status='sending' AND lease_token=$6::uuid AND next_attempt_at>NOW()`, id, status, errText, retry, retryAfter.Microseconds(), token)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return nil
	}
	if retry {
		payload, marshalErr := json.Marshal(map[string]int64{"notification_id": id})
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload,next_attempt_at) VALUES($1,'notification',$2,$3,NOW()+($4*INTERVAL '1 microsecond'))`, notificationEvent, id, payload, retryAfter.Microseconds()); err != nil {
			return err
		}
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, NotificationConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *MonitoringExecutionRepository) SkipNotification(ctx context.Context, id int64, messageID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE notifications SET status='skipped',last_error='contact is no longer active or verified' WHERE id=$1 AND status='pending'`, id); err != nil {
		return err
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, NotificationConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MonitoringExecutionRepository) DisableContactForPermanentNotificationFailure(ctx context.Context, notificationID int64, token, contact string, contactUpdatedAt time.Time, messageID, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var contactID int64
	if err := tx.QueryRowContext(ctx, `UPDATE notifications SET status='failed',lease_token=NULL,last_error=$2 WHERE id=$1 AND status='sending' AND lease_token=$3::uuid AND next_attempt_at>NOW() RETURNING user_contact_id`, notificationID, reason, token).Scan(&contactID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE user_contacts SET is_active=FALSE,updated_at=NOW() WHERE id=$1 AND contact=$2 AND updated_at=$3`, contactID, contact, contactUpdatedAt)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE notifications SET status='skipped',lease_token=NULL,last_error='contact disabled after permanent delivery failure' WHERE user_contact_id=$1 AND status IN ('pending','sending') AND id<>$2`, contactID, notificationID); err != nil {
			return err
		}
	}
	if _, err = NewProcessedMessageRepository().RecordOnce(ctx, tx, NotificationConsumer, messageID); err != nil {
		return err
	}
	return tx.Commit()
}
