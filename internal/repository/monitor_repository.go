package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lib/pq"

	"monitoring_service/internal/domain"
)

type MonitorRepository struct {
	db *sql.DB
}

var (
	ErrMonitorRuleLimit           = errors.New("monitor rule limit reached")
	ErrMonitorNotResumable        = errors.New("monitor cannot be resumed from current status")
	ErrEndpointTypeConflict       = errors.New("monitored URL already has another content type")
	ErrEndpointHeadersConflict    = errors.New("monitored URL already has another header set")
	ErrEndpointPageConflict       = errors.New("monitored URL already has another source page")
	ErrEndpointJSONSourceConflict = errors.New("monitored URL already has another JSON source")
	ErrTargetHostnameUnavailable  = errors.New("target hostname is unavailable")
)

func NewMonitorRepository(db *sql.DB) *MonitorRepository {
	return &MonitorRepository{db: db}
}

const monitorSelect = `
    SELECT m.id, m.user_id, m.endpoint_id, e.canonical_url, m.name, e.type, '{}'::jsonb /* caller headers disabled */, m.page_url, COALESCE(e.browser_page_url,''), COALESCE(e.json_source,'null'::jsonb), m.path_type, m.rules_count_to_finish, m.status,
           m.last_checked_at, m.last_error, m.completed_at, m.stopped_at,
           m.deleted_at, m.created_at, m.updated_at, m.session_mode, m.session_status, m.session_ciphertext IS NOT NULL
    FROM monitors m
    JOIN monitored_endpoints e ON e.id = m.endpoint_id
`

func (r *MonitorRepository) Create(ctx context.Context, input domain.CreateMonitorInput) (domain.Monitor, error) {
	if input.SessionMode == "" {
		input.SessionMode = "managed"
	}
	if input.EndpointType == "" {
		input.EndpointType = domain.EndpointTypeJSON
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("begin monitor creation: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	if input.TargetHostnameID != 0 {
		var targetID int64
		if err = tx.QueryRowContext(ctx, `
			SELECT id FROM target_hostnames
			WHERE id = $1 AND is_active
			FOR KEY SHARE
		`, input.TargetHostnameID).Scan(&targetID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.Monitor{}, ErrTargetHostnameUnavailable
			}
			return domain.Monitor{}, fmt.Errorf("lock target hostname: %w", err)
		}
	}

	var endpointID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO monitored_endpoints (canonical_url, type, headers, headers_fingerprint, target_hostname_id, browser_page_url, json_source)
		VALUES ($1, $2, $3::jsonb, $4, $5, NULLIF($6,''), $7::jsonb)
        ON CONFLICT (canonical_url) DO NOTHING
        RETURNING id
	`, input.CanonicalURL, input.EndpointType, "{}", "", input.TargetHostnameID, input.BrowserPageURL, marshalJSONSource(input.JSONSource)).Scan(&endpointID)
	if err != nil {
		if err != sql.ErrNoRows {
			return domain.Monitor{}, fmt.Errorf("create monitored endpoint: %w", err)
		}
		var endpointType, endpointStatus, headersFingerprint, pageURL string
		var sourceMatches bool
		if err = tx.QueryRowContext(ctx, `
			SELECT id, type, status, headers_fingerprint, COALESCE(browser_page_url,''), json_source IS NOT DISTINCT FROM $2::jsonb FROM monitored_endpoints WHERE canonical_url = $1 FOR UPDATE
		`, input.CanonicalURL, marshalJSONSource(input.JSONSource)).Scan(&endpointID, &endpointType, &endpointStatus, &headersFingerprint, &pageURL, &sourceMatches); err != nil {
			return domain.Monitor{}, fmt.Errorf("get monitored endpoint: %w", err)
		}
		if endpointType != input.EndpointType {
			return domain.Monitor{}, ErrEndpointTypeConflict
		}
		// Headers now belong to the monitor; canonical URLs may be shared.
		if pageURL != input.BrowserPageURL {
			return domain.Monitor{}, ErrEndpointPageConflict
		}
		if !sourceMatches {
			return domain.Monitor{}, ErrEndpointJSONSourceConflict
		}

	}

	var monitor domain.Monitor
	if err = tx.QueryRowContext(ctx, `
        INSERT INTO monitors (user_id, endpoint_id, name, path_type, rules_count_to_finish, headers, session_mode, session_ciphertext, session_status, page_url)
        VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, NULLIF($8,''), CASE WHEN $8 = '' THEN 'missing' ELSE 'ready' END, $9)
        RETURNING id, user_id, endpoint_id, name, path_type, rules_count_to_finish, status, created_at, updated_at
    `, input.UserID, endpointID, input.Name, input.PathType, input.RulesCountToFinish, mustMarshalHeaders(input.Headers), input.SessionMode, input.SessionCiphertext, input.PageURL).Scan(
		&monitor.ID,
		&monitor.UserID,
		&monitor.EndpointID,
		&monitor.Name,
		&monitor.PathType,
		&monitor.RulesCountToFinish,
		&monitor.Status,
		&monitor.CreatedAt,
		&monitor.UpdatedAt,
	); err != nil {
		return domain.Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	monitor.URL = input.CanonicalURL
	monitor.PageURL = input.PageURL
	monitor.BrowserPageURL = input.BrowserPageURL
	monitor.JSONSource = input.JSONSource
	monitor.EndpointType = input.EndpointType
	monitor.Headers = input.Headers
	monitor.SessionMode = input.SessionMode
	monitor.HasSession = input.SessionCiphertext != ""
	monitor.SessionStatus = "missing"
	if input.IdempotencyKey != "" {
		result, err := tx.ExecContext(ctx, `UPDATE idempotency_keys SET monitor_id=$1 WHERE key=$2 AND request_hash=$3 AND user_id=$4 AND status='processing' AND expires_at>NOW() AND monitor_id IS NULL`, monitor.ID, input.IdempotencyKey, input.IdempotencyHash, input.UserID)
		if err != nil {
			return domain.Monitor{}, fmt.Errorf("link monitor operation: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return domain.Monitor{}, err
		}
		if n != 1 {
			return domain.Monitor{}, fmt.Errorf("monitor operation is no longer active")
		}
	}
	if monitor.HasSession {
		monitor.SessionStatus = "ready"
	}

	if len(input.Rules) > 0 {
		payload, err := json.Marshal(input.Rules)
		if err != nil {
			return domain.Monitor{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO monitor_rules(monitor_id,json_path,comparison_operator,expected_value,source,number_format,description)
 SELECT $1,r."JSONPath",r."ComparisonOperator",r."ExpectedValue",NULLIF(r."Source",'null'::jsonb),COALESCE(NULLIF(r."NumberFormat",''),'number'),r."Description"
 FROM jsonb_to_recordset($2::jsonb) AS r("JSONPath" text,"ComparisonOperator" text,"ExpectedValue" double precision,"Source" jsonb,"NumberFormat" text,"Description" text)`, monitor.ID, payload); err != nil {
			return domain.Monitor{}, fmt.Errorf("create monitor rules: %w", err)
		}
	}
	rows, err := tx.QueryContext(ctx, `
        SELECT id, monitor_id, json_path, comparison_operator, expected_value, created_at, source, number_format, description
        FROM monitor_rules
        WHERE monitor_id = $1
        ORDER BY id
    `, monitor.ID)
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("list created monitor rules: %w", err)
	}
	for rows.Next() {
		var rule domain.MonitorRule
		var rawSource []byte
		if err := rows.Scan(
			&rule.ID,
			&rule.MonitorID,
			&rule.JSONPath,
			&rule.ComparisonOperator,
			&rule.ExpectedValue,
			&rule.CreatedAt, &rawSource, &rule.NumberFormat, &rule.Description,
		); err != nil {
			_ = rows.Close()
			return domain.Monitor{}, err
		}
		if len(rawSource) > 0 {
			if err := json.Unmarshal(rawSource, &rule.Source); err != nil {
				return domain.Monitor{}, err
			}
		}
		monitor.Rules = append(monitor.Rules, rule)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.Monitor{}, err
	}
	if err := rows.Close(); err != nil {
		return domain.Monitor{}, err
	}

	if err = tx.Commit(); err != nil {
		return domain.Monitor{}, fmt.Errorf("commit monitor creation: %w", err)
	}
	return monitor, nil
}

func (r *MonitorRepository) ListByUser(ctx context.Context, userID int64, limit int, cursor *domain.MonitorCursor) ([]domain.Monitor, error) {
	var cursorCreatedAt any
	var cursorID any
	if cursor != nil {
		cursorCreatedAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := r.db.QueryContext(ctx, monitorSelect+`
        WHERE m.user_id = $1 AND m.deleted_at IS NULL
          AND ($2::timestamptz IS NULL OR (m.created_at, m.id) < ($2, $3))
        ORDER BY m.created_at DESC, m.id DESC
        LIMIT $4
    `, userID, cursorCreatedAt, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()

	monitors := make([]domain.Monitor, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		monitor, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, monitor)
		ids = append(ids, monitor.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rules, err := r.ListRules(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range monitors {
		monitors[i].Rules = rules[monitors[i].ID]
	}
	return monitors, nil
}

func (r *MonitorRepository) AddRule(ctx context.Context, userID, monitorID int64, input domain.CreateMonitorRuleInput) (domain.MonitorRule, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.MonitorRule{}, fmt.Errorf("begin add monitor rule: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()

	var lockedID int64
	if err = tx.QueryRowContext(ctx, `
        SELECT id FROM monitors
        WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
        FOR UPDATE
    `, monitorID, userID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.MonitorRule{}, sql.ErrNoRows
		}
		return domain.MonitorRule{}, fmt.Errorf("lock monitor for rule creation: %w", err)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM monitor_rules WHERE monitor_id = $1`, monitorID).Scan(&count); err != nil {
		return domain.MonitorRule{}, fmt.Errorf("count monitor rules: %w", err)
	}
	if count >= 3 {
		return domain.MonitorRule{}, ErrMonitorRuleLimit
	}
	var rule domain.MonitorRule
	if err = tx.QueryRowContext(ctx, `
        INSERT INTO monitor_rules (monitor_id, json_path, comparison_operator, expected_value, source, number_format, description)
        VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
        RETURNING id, monitor_id, json_path, comparison_operator, expected_value, created_at, description
    `, monitorID, input.JSONPath, input.ComparisonOperator, input.ExpectedValue, marshalRuleSource(input.Source), ruleNumberFormat(input.NumberFormat), input.Description).Scan(
		&rule.ID, &rule.MonitorID, &rule.JSONPath, &rule.ComparisonOperator, &rule.ExpectedValue, &rule.CreatedAt, &rule.Description,
	); err != nil {
		return domain.MonitorRule{}, fmt.Errorf("create monitor rule: %w", err)
	}
	if err = invalidateMonitorConfiguration(ctx, tx, monitorID); err != nil {
		return domain.MonitorRule{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.MonitorRule{}, fmt.Errorf("commit monitor rule: %w", err)
	}
	rule.Source = input.Source
	rule.NumberFormat = ruleNumberFormat(input.NumberFormat)
	return rule, nil
}

func (r *MonitorRepository) GetByUser(ctx context.Context, userID, id int64) (domain.Monitor, error) {
	monitor, err := scanMonitor(r.db.QueryRowContext(ctx, monitorSelect+`
        WHERE m.user_id = $1 AND m.id = $2 AND m.deleted_at IS NULL
    `, userID, id))
	if err != nil {
		return domain.Monitor{}, err
	}
	rules, err := r.ListRules(ctx, []int64{id})
	if err != nil {
		return domain.Monitor{}, err
	}
	monitor.Rules = rules[id]
	return monitor, nil
}

func (r *MonitorRepository) ListRules(ctx context.Context, monitorIDs []int64) (map[int64][]domain.MonitorRule, error) {
	rulesByMonitor := make(map[int64][]domain.MonitorRule, len(monitorIDs))
	if len(monitorIDs) == 0 {
		return rulesByMonitor, nil
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, monitor_id, json_path, comparison_operator, expected_value, created_at, source, number_format, description
        FROM monitor_rules
        WHERE monitor_id = ANY($1)
        ORDER BY monitor_id, id
    `, pq.Array(monitorIDs))
	if err != nil {
		return nil, fmt.Errorf("list monitor rules: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.ErrorContext(ctx, "close query rows", "error", err)
		}
	}()
	for rows.Next() {
		var rule domain.MonitorRule
		var rawSource []byte
		if err := rows.Scan(
			&rule.ID,
			&rule.MonitorID,
			&rule.JSONPath,
			&rule.ComparisonOperator,
			&rule.ExpectedValue,
			&rule.CreatedAt, &rawSource, &rule.NumberFormat, &rule.Description,
		); err != nil {
			return nil, err
		}
		if len(rawSource) > 0 {
			if err := json.Unmarshal(rawSource, &rule.Source); err != nil {
				return nil, err
			}
		}
		rulesByMonitor[rule.MonitorID] = append(rulesByMonitor[rule.MonitorID], rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rulesByMonitor, nil
}

func (r *MonitorRepository) Stop(ctx context.Context, userID, id int64) error {
	return r.updateOwnedAndCancelFetches(ctx, `
        UPDATE monitors
        SET status = 'stopped', stopped_at = COALESCE(stopped_at, NOW()), generation=generation+1, check_status='idle', refresh_lease_token=NULL,refresh_lease_until=NULL,updated_at = NOW()
        WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL
          AND status IN ('monitoring', 'stopped')
    `, userID, id)
}

func (r *MonitorRepository) Resume(ctx context.Context, userID, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	var state, mode, sessionStatus, checkStatus string
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT m.status,m.session_mode,m.session_status,m.check_status,t.is_active FROM monitors m
 JOIN monitored_endpoints e ON e.id=m.endpoint_id JOIN target_hostnames t ON t.id=e.target_hostname_id
 WHERE m.user_id=$1 AND m.id=$2 AND m.deleted_at IS NULL FOR UPDATE OF m`, userID, id).Scan(&state, &mode, &sessionStatus, &checkStatus, &active)
	if err != nil {
		return err
	}
	if !active {
		return ErrTargetHostnameUnavailable
	}
	if state != "monitoring" && state != "stopped" && state != "error" {
		return ErrMonitorNotResumable
	}
	if mode == "provided" && sessionStatus != "ready" {
		return ErrMonitorNotResumable
	}
	if state == "monitoring" && (checkStatus == "scheduled" || checkStatus == "fetching") {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE monitors SET status='monitoring',check_status='idle',next_check_at=NOW(),
 generation=generation+1,attempts_count=0,consecutive_errors=0,retry_started_at=NULL,
 last_error=NULL,completed_at=NULL,stopped_at=NULL,refresh_lease_token=NULL,refresh_lease_until=NULL,
 session_status=CASE WHEN session_ciphertext IS NULL THEN 'missing' ELSE 'ready' END,updated_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Replacing credentials invalidates in-flight results without silently resuming a terminal monitor.
func (r *MonitorRepository) ReplaceSession(ctx context.Context, userID, id int64, mode, ciphertext string) (domain.Monitor, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Monitor{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	result, err := tx.ExecContext(ctx, `UPDATE monitors SET session_mode=$3,session_ciphertext=NULLIF($4,''),
 session_status=CASE WHEN $4='' THEN 'missing' ELSE 'ready' END,session_generation=session_generation+1,profile_generation=profile_generation+1,
 generation=generation+1,session_updated_at=NOW(),last_session_error=NULL,refresh_lease_token=NULL,
 refresh_lease_until=NULL,check_status='idle',next_check_at=NOW(),attempts_count=0,updated_at=NOW()
 WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, userID, id, mode, ciphertext)
	if err != nil {
		return domain.Monitor{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return domain.Monitor{}, err
	}
	if n != 1 {
		return domain.Monitor{}, sql.ErrNoRows
	}
	if err = cancelMonitorFetches(ctx, tx, id); err != nil {
		return domain.Monitor{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Monitor{}, err
	}
	return r.GetByUser(ctx, userID, id)
}

func (r *MonitorRepository) SoftDelete(ctx context.Context, userID, id int64) error {
	return r.updateOwnedAndCancelFetches(ctx, `
        UPDATE monitors
        SET deleted_at = NOW(), generation=generation+1,refresh_lease_token=NULL,refresh_lease_until=NULL,updated_at = NOW()
        WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL
    `, userID, id)
}

func cancelMonitorFetches(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='failed',finished_at=NOW(),lease_token=NULL,lease_until=NULL,last_error='monitor operation superseded fetch' WHERE monitor_id=$1 AND status IN ('pending','fetching')`, id)
	return err
}

func (r *MonitorRepository) updateOwnedAndCancelFetches(ctx context.Context, query string, userID, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	result, err := tx.ExecContext(ctx, query, userID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if err = cancelMonitorFetches(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MonitorRepository) UpdateName(ctx context.Context, userID, id int64, name string) (domain.Monitor, error) {
	result, err := r.db.ExecContext(ctx, `
        UPDATE monitors
        SET name = $3, updated_at = NOW()
        WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL
    `, userID, id, name)
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("update monitor name: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Monitor{}, err
	}
	if affected != 1 {
		return domain.Monitor{}, sql.ErrNoRows
	}
	return r.GetByUser(ctx, userID, id)
}

func (r *MonitorRepository) UpdateRulesCountToFinish(ctx context.Context, userID, id int64, rulesCountToFinish int) (domain.Monitor, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("begin monitor threshold update: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slog.Error("rollback transaction", "error", err)
		}
	}()
	result, err := tx.ExecContext(ctx, `
        UPDATE monitors
        SET rules_count_to_finish = $3, updated_at = NOW()
        WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL
    `, userID, id, rulesCountToFinish)
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("update monitor rule threshold: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Monitor{}, err
	}
	if affected != 1 {
		return domain.Monitor{}, sql.ErrNoRows
	}
	if err = invalidateMonitorConfiguration(ctx, tx, id); err != nil {
		return domain.Monitor{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Monitor{}, fmt.Errorf("commit monitor threshold update: %w", err)
	}
	return r.GetByUser(ctx, userID, id)
}

// Lock the monitor before its jobs, matching the worker finalization order.
// Generations must never be reused: a delayed delivery may survive a rule edit.
func invalidateMonitorConfiguration(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE monitors SET generation=generation+1,
 check_status='idle', attempts_count=0, consecutive_errors=0, retry_started_at=NULL,
 last_error=CASE WHEN status='monitoring' THEN NULL ELSE last_error END,
 next_check_at=CASE WHEN status='monitoring' THEN NOW() ELSE next_check_at END,
 refresh_lease_token=NULL,refresh_lease_until=NULL,
 session_status=CASE WHEN session_status='refreshing' THEN CASE WHEN session_ciphertext IS NULL THEN 'missing' ELSE 'ready' END ELSE session_status END,
 updated_at=NOW() WHERE id=$1`, id); err != nil {
		return fmt.Errorf("invalidate monitor configuration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE endpoint_fetches SET status='failed',finished_at=NOW(),
 lease_token=NULL,lease_until=NULL,last_error='monitor configuration changed'
 WHERE monitor_id=$1 AND status IN ('pending','fetching')`, id); err != nil {
		return fmt.Errorf("supersede monitor fetches: %w", err)
	}
	return nil
}

type monitorScanner interface {
	Scan(...any) error
}

func scanMonitor(scanner monitorScanner) (domain.Monitor, error) {
	var (
		monitor       domain.Monitor
		rawHeaders    []byte
		rawJSONSource []byte
		lastCheckedAt sql.NullTime
		lastError     sql.NullString
		completedAt   sql.NullTime
		stoppedAt     sql.NullTime
		deletedAt     sql.NullTime
	)
	err := scanner.Scan(
		&monitor.ID,
		&monitor.UserID,
		&monitor.EndpointID,
		&monitor.URL,
		&monitor.Name,
		&monitor.EndpointType,
		&rawHeaders,
		&monitor.PageURL,
		&monitor.BrowserPageURL,
		&rawJSONSource,
		&monitor.PathType,
		&monitor.RulesCountToFinish,
		&monitor.Status,
		&lastCheckedAt,
		&lastError,
		&completedAt,
		&stoppedAt,
		&deletedAt,
		&monitor.CreatedAt,
		&monitor.UpdatedAt,
		&monitor.SessionMode, &monitor.SessionStatus, &monitor.HasSession,
	)
	if err != nil {
		return domain.Monitor{}, err
	}
	if err = json.Unmarshal(rawHeaders, &monitor.Headers); err != nil {
		return domain.Monitor{}, fmt.Errorf("decode monitor endpoint headers: %w", err)
	}
	if err = json.Unmarshal(rawJSONSource, &monitor.JSONSource); err != nil {
		return domain.Monitor{}, fmt.Errorf("decode monitor JSON source: %w", err)
	}
	monitor.LastCheckedAt = nullTimePtr(lastCheckedAt)
	monitor.LastError = nullStringPtr(lastError)
	monitor.CompletedAt = nullTimePtr(completedAt)
	monitor.StoppedAt = nullTimePtr(stoppedAt)
	monitor.DeletedAt = nullTimePtr(deletedAt)
	return monitor, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
func mustMarshalHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	body, _ := json.Marshal(headers)
	return string(body)
}

func marshalJSONSource(source *domain.JSONSource) any {
	if source == nil {
		return nil
	}
	value, _ := json.Marshal(source) // A closed struct containing only strings.
	return string(value)
}
func marshalRuleSource(source *domain.RuleSource) any {
	if source == nil {
		return nil
	}
	body, _ := json.Marshal(source)
	return string(body)
}
func ruleNumberFormat(format string) string {
	if format == "" {
		return "number"
	}
	return format
}
