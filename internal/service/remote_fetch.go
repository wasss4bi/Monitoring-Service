package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/google/uuid"
	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/metrics"
	"monitoring_service/internal/repository"
)

type ExtractionClient interface {
	Fetch(context.Context, *v1.FetchRequest) (*v1.FetchResponse, error)
	Prepare(context.Context, *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error)
}

const sessionPreparationRPCLimit = 100 * time.Second

func (s *MonitoringExecutionService) WithFetcher(client ExtractionClient) *MonitoringExecutionService {
	s.fetcher = client
	return s
}

func makeExtractionRequest(job domain.FetchJob, monitors []repository.EvaluationMonitor) *v1.FetchRequest {
	r := &v1.FetchRequest{RequestId: uuid.NewString(), Url: job.CanonicalURL, EndpointType: v1.EndpointType_ENDPOINT_TYPE_JSON, Mode: v1.FetchMode_FETCH_MODE_HTTP_JSON}
	if job.EndpointType == "html" {
		r.EndpointType = v1.EndpointType_ENDPOINT_TYPE_HTML
		r.Mode = v1.FetchMode_FETCH_MODE_RENDERED_HTML
	}
	if job.BrowserPageURL != "" {
		r.Mode = v1.FetchMode_FETCH_MODE_BROWSER_JSON_CAPTURE
		r.SourcePageUrl = job.BrowserPageURL
	}
	if source := job.JSONSource; source != nil {
		r.Mode = v1.FetchMode_FETCH_MODE_INLINE_JSON
		r.SourcePageUrl = job.PageURL
		r.InlineJsonSource = &v1.InlineJsonSource{ScriptContains: source.ScriptContains, ObjectKey: source.ObjectKey, MatchPath: source.Match.Path, MatchValue: source.Match.Value}
	}
	for _, m := range monitors {
		for _, rule := range m.Rules {
			kind := v1.SelectorKind_SELECTOR_KIND_JSON_PATH
			switch m.PathType {
			case "regex":
				kind = v1.SelectorKind_SELECTOR_KIND_REGEX
			case "xpath":
				kind = v1.SelectorKind_SELECTOR_KIND_XPATH
			case "selector":
				kind = v1.SelectorKind_SELECTOR_KIND_CSS
			}
			selector := &v1.Selector{Id: strconv.FormatInt(rule.ID, 10), Kind: kind, Expression: rule.JSONPath}
			if rule.Source != nil {
				selector.Source = &v1.RuleSource{Path: rule.Source.Path, KeyPrefix: rule.Source.KeyPrefix, DecodeJson: rule.Source.DecodeJSON}
			}
			r.Selectors = append(r.Selectors, selector)
		}
	}
	mode := v1.SessionMode_SESSION_MODE_NONE
	if job.BootstrapURL != "" || job.BrowserPageURL != "" || job.JSONSource != nil || job.EndpointType == "html" || job.SessionCiphertext != "" || job.SessionMode == "provided" {
		mode = v1.SessionMode_SESSION_MODE_MANAGED_ANONYMOUS
		if job.SessionMode == "provided" {
			mode = v1.SessionMode_SESSION_MODE_PROVIDED_ANONYMOUS
		}
	}
	r.Session = &v1.SessionContext{Mode: mode, ProfileScope: monitorProfileScope(job), Generation: job.SessionGeneration}
	return r
}

func (s *MonitoringExecutionService) remotePermit(ctx context.Context, job domain.FetchJob) (time.Duration, error) {
	return s.limiter.AcquireKey(ctx, "monitor_target:"+strconv.FormatInt(job.TargetHostnameID, 10), job.TargetLimitPerMin, time.Minute)
}

func (s *MonitoringExecutionService) preparationPermit(ctx context.Context, job domain.FetchJob) (time.Duration, error) {
	wait, err := s.remotePermit(ctx, job)
	deadline, bounded := ctx.Deadline()
	if err != nil || wait <= 0 || !bounded || time.Until(deadline) <= wait+sessionPreparationRPCLimit {
		return wait, err
	}
	// After a real session rejection, restarting from Fetch at each new window
	// would spend a one/minute quota rejecting the same session forever. Keep
	// this refresh lease only when the existing attempt budget fits the wait
	// and a full preparation RPC; no database transaction is held here.
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return s.remotePermit(ctx, job)
	}
}

func (s *MonitoringExecutionService) processRemoteFetch(lifecycle context.Context, fetchID, messageID string) (resultErr error) {
	started := time.Now()
	processed, err := s.repo.IsProcessed(lifecycle, repository.EndpointFetchConsumer, messageID)
	if err != nil || processed {
		return err
	}
	token := uuid.NewString()
	job, err := s.repo.ClaimFetch(lifecycle, fetchID, token, max(s.lease, 330*time.Second))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	browser := job.EndpointType == "html" || job.BrowserPageURL != "" || job.BootstrapURL != "" || job.JSONSource != nil
	budget, reserve := 15*time.Second, time.Second
	if browser {
		budget, reserve = 300*time.Second, 5*time.Second
	}
	attempt, stop := context.WithTimeout(lifecycle, budget)
	defer stop()
	work, cancel := context.WithTimeout(attempt, budget-reserve)
	defer cancel()
	finalize := func(fn func(context.Context) error) error {
		ctx, done := context.WithTimeout(lifecycle, reserve)
		defer done()
		return fn(ctx)
	}
	deferJob := func(reason string, delay time.Duration) error {
		metrics.FetcherEvent("monitoring_defer", reason)
		return finalize(func(ctx context.Context) error {
			return s.repo.DeferInfrastructure(ctx, fetchID, token, messageID, reason, delay)
		})
	}
	finishConfiguration := func() error {
		return finalize(func(ctx context.Context) error {
			return s.repo.FinalizeObservation(ctx, fetchID, token, messageID, repository.FetchObservation{Metadata: []byte(`{"configuration_error":true}`), Terminal: true, Message: "invalid extraction configuration"})
		})
	}
	monitors, err := s.repo.ActiveMonitors(work, job.MonitorID)
	if err != nil {
		return err
	}
	request := makeExtractionRequest(job, monitors)
	if len(request.Selectors) == 0 {
		return deferJob("configuration_unavailable", time.Second)
	}
	if job.SessionCiphertext != "" {
		if s.targetSessionProtector == nil {
			return deferJob("session_encryption_unavailable", 2*time.Second)
		}
		plain, decryptErr := s.targetSessionProtector.Decrypt(job.SessionCiphertext)
		if decryptErr != nil {
			return deferJob("session_decryption_failed", 2*time.Second)
		}
		request.Session.State = []byte(plain)
	}
	var response *v1.FetchResponse
	rpcCalls := 0
	for tries := 0; tries < 3; tries++ {
		wait, permitErr := s.remotePermit(work, job)
		if permitErr != nil {
			return deferJob("cancelled", time.Second)
		}
		if wait > 0 {
			return deferJob("hostname_limit", wait)
		}
		rpcLimit := 5 * time.Second
		if request.Mode == v1.FetchMode_FETCH_MODE_RENDERED_HTML || request.Mode == v1.FetchMode_FETCH_MODE_BROWSER_JSON_CAPTURE {
			rpcLimit = 100 * time.Second
		}
		rpcCtx, done := context.WithTimeout(work, rpcLimit)
		rpcCalls++
		response, err = s.fetcher.Fetch(rpcCtx, request)
		done()
		if err == nil {
			break
		}
		code := status.Code(err)
		if code != codes.Unavailable || tries == 2 {
			break
		}
		jitter := time.Duration(rand.Int64N(int64((100*time.Millisecond)<<tries) + 1)) // #nosec G404 -- randomness only spreads retry delays and is not used for security
		select {
		case <-work.Done():
			return deferJob("fetcher_deadline", 2*time.Second)
		case <-time.After(jitter):
		}
	}
	if err != nil {
		if status.Code(err) == codes.InvalidArgument {
			return finishConfiguration()
		}
		return deferJob("fetcher_"+status.Code(err).String(), 2*time.Second)
	}
	if response == nil {
		return deferJob("fetcher_invalid_response", 2*time.Second)
	}
	needsSession := func(r *v1.FetchResponse) bool {
		return r.Failure != nil && (r.Failure.Code == v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REQUIRED || r.Failure.Code == v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REJECTED || r.Failure.Code == v1.FetchErrorCode_FETCH_ERROR_CODE_PROFILE_UNAVAILABLE)
	}
	preparationFailed := false
	if needsSession(response) && request.Session.Mode == v1.SessionMode_SESSION_MODE_MANAGED_ANONYMOUS {
		refreshToken := uuid.NewString()
		acquired, err := s.repo.AcquireSessionRefresh(work, job.MonitorID, job.SessionGeneration, refreshToken, 330*time.Second)
		if err != nil {
			return err
		}
		if !acquired {
			return deferJob("session_refresh_busy", 2*time.Second)
		}
		refreshHeld := true
		defer func() {
			if refreshHeld {
				releaseErr := finalize(func(ctx context.Context) error { return s.repo.ReleaseSessionRefresh(ctx, job.MonitorID, refreshToken) })
				resultErr = errors.Join(resultErr, releaseErr)
			}
		}()

		source := job.BootstrapURL
		mode := v1.SessionPreparationMode_SESSION_PREPARATION_MODE_GUEST_BOOTSTRAP
		if job.BrowserPageURL != "" {
			source = job.BrowserPageURL
			mode = v1.SessionPreparationMode_SESSION_PREPARATION_MODE_BROWSER_CAPTURE
		}
		if job.JSONSource != nil {
			source = job.PageURL
			mode = v1.SessionPreparationMode_SESSION_PREPARATION_MODE_INLINE_JSON
		}
		if source == "" {
			source = job.CanonicalURL
		}
		rules, err := s.repo.BootstrapRules(work, job.TargetHostnameID)
		if err != nil {
			return err
		}
		var policy []struct {
			Origin     string   `json:"origin"`
			PathPrefix string   `json:"path_prefix"`
			Methods    []string `json:"methods"`
		}
		if err = json.Unmarshal(rules, &policy); err != nil {
			return deferJob("bootstrap_policy_invalid", 2*time.Second)
		}
		prepare := &v1.PrepareSessionRequest{RequestId: uuid.NewString(), EndpointUrl: job.CanonicalURL, SourcePageUrl: source, Mode: mode, CurrentSession: request.Session, InlineJsonSource: request.InlineJsonSource}
		for _, rule := range policy {
			prepare.BootstrapRules = append(prepare.BootstrapRules, &v1.BootstrapRequestRule{Origin: rule.Origin, PathPrefix: rule.PathPrefix, Methods: rule.Methods})
		}
		if rpcCalls >= 3 {
			return deferJob("rpc_budget", time.Second)
		}
		// A session discovery response can explicitly report no target request.
		// Use its reserved permit for preparation, otherwise a one/minute target
		// repeatedly spends its only permit discovering the same missing session.
		if response.Fetch == nil || response.Fetch.NetworkPerformed {
			wait, err := s.preparationPermit(work, job)
			if err != nil || wait > 0 {
				return deferJob("hostname_limit", max(wait, time.Second))
			}
		}
		prepareCtx, done := context.WithTimeout(work, sessionPreparationRPCLimit)
		rpcCalls++
		prepared, err := s.fetcher.Prepare(prepareCtx, prepare)
		done()
		if err != nil && status.Code(err) == codes.InvalidArgument {
			return finishConfiguration()
		}
		if err != nil || prepared == nil {
			return deferJob("session_engine_unavailable", 2*time.Second)
		}
		if prepared.Failure != nil {
			preparationFailed = true
			response = &v1.FetchResponse{RequestId: request.RequestId, Failure: prepared.Failure, Fetch: &v1.FetchSummary{NetworkPerformed: true}}
			for _, sel := range request.Selectors {
				response.Results = append(response.Results, &v1.SelectorResult{Id: sel.Id, Outcome: &v1.SelectorResult_Error{Error: &v1.ExtractionError{Code: v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_EVALUATED}}})
			}
		} else {
			if !json.Valid(prepared.SessionState) || len(prepared.SessionState) > 256<<10 {
				return deferJob("session_engine_invalid_response", 2*time.Second)
			}
			cipher, err := s.targetSessionProtector.Encrypt(string(prepared.SessionState))
			if err != nil {
				return err
			}
			generation, err := s.repo.SavePreparedSession(work, fetchID, token, refreshToken, job.SessionGeneration, cipher)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			refreshHeld = false
			job.SessionGeneration = generation
			request.Session.Generation = generation
			request.Session.State = prepared.SessionState
			request.RequestId = uuid.NewString()
			wait, err := s.remotePermit(work, job)
			if err != nil || wait > 0 {
				return deferJob("hostname_limit", max(wait, time.Second))
			}
			limit := 5 * time.Second
			if request.Mode == v1.FetchMode_FETCH_MODE_BROWSER_JSON_CAPTURE || request.Mode == v1.FetchMode_FETCH_MODE_RENDERED_HTML {
				limit = 100 * time.Second
			}
			rpcCtx, done := context.WithTimeout(work, limit)
			if rpcCalls >= 3 {
				done()
				return deferJob("rpc_budget", time.Second)
			}
			response, err = s.fetcher.Fetch(rpcCtx, request)
			done()
			if err != nil && status.Code(err) == codes.InvalidArgument {
				return finishConfiguration()
			}
			if err != nil || response == nil {
				return deferJob("fetcher_unavailable_after_prepare", 2*time.Second)
			}
		}
	}
	observation, err := evaluateRemoteResponse(request, response, monitors, job.AttemptsCount, s.interval)
	if err != nil {
		return deferJob("fetcher_invalid_response", 2*time.Second)
	}
	observation.SessionGeneration = job.SessionGeneration
	observation.RejectSession = request.Session.Mode == v1.SessionMode_SESSION_MODE_PROVIDED_ANONYMOUS && needsSession(response)
	if preparationFailed && observation.AccessDenied {
		observation.RejectSession = true
		observation.SessionError = "session preparation rejected"
	}
	if response.Failure == nil && len(response.UpdatedSessionState) > 0 {
		if s.targetSessionProtector == nil || !json.Valid(response.UpdatedSessionState) || len(response.UpdatedSessionState) > 256<<10 {
			return deferJob("session_engine_invalid_response", 2*time.Second)
		}
		observation.SessionCiphertext, err = s.targetSessionProtector.Encrypt(string(response.UpdatedSessionState))
		if err != nil {
			return err
		}
	}
	err = finalize(func(ctx context.Context) error {
		return s.repo.FinalizeObservation(ctx, fetchID, token, messageID, observation)
	})
	if err == nil && observation.Observed {
		outcome := "success"
		if observation.Failure {
			outcome = "target_failure"
		}
		metrics.ObserveMonitorFetch(outcome, time.Since(started))
	}
	return err
}

func evaluateRemoteResponse(request *v1.FetchRequest, response *v1.FetchResponse, monitors []repository.EvaluationMonitor, attempts int64, interval time.Duration) (repository.FetchObservation, error) {
	out := repository.FetchObservation{NextCheck: interval, Observed: response.Fetch != nil && response.Fetch.NetworkPerformed}
	for _, item := range response.Results {
		if item.GetValue() != nil {
			out.Observed = true
		}
	}
	if response.Fetch != nil && response.Fetch.HttpStatus != nil {
		n := int(*response.Fetch.HttpStatus)
		if n < 100 || n > 599 {
			return out, errors.New("invalid HTTP status")
		}
		out.HTTPStatus = &n
	}
	metadata := proto.Clone(response).(*v1.FetchResponse)
	metadata.UpdatedSessionState = nil
	var err error
	out.Metadata, err = protojson.Marshal(metadata)
	if err != nil {
		return out, err
	}
	if response.RequestId != request.RequestId || len(response.Results) != len(request.Selectors) {
		return out, errors.New("invalid response correlation")
	}
	values := map[string]*v1.SelectorResult{}
	expected := map[string]bool{}
	for _, s := range request.Selectors {
		expected[s.Id] = true
	}
	for _, item := range response.Results {
		if item == nil || !expected[item.Id] || values[item.Id] != nil || item.Outcome == nil {
			return out, errors.New("invalid result IDs")
		}
		values[item.Id] = item
	}
	if response.Failure != nil {
		out.Failure = true
		out.AccessDenied = response.Failure.Code == v1.FetchErrorCode_FETCH_ERROR_CODE_SESSION_REJECTED
		out.Message = response.Failure.Code.String()
		if response.Failure.HttpStatus != nil {
			n := int(*response.Failure.HttpStatus)
			if n < 100 || n > 599 {
				return out, errors.New("invalid HTTP status")
			}
			out.HTTPStatus = &n
		}
		retry := false
		switch response.Failure.Code {
		case v1.FetchErrorCode_FETCH_ERROR_CODE_DNS_FAILURE, v1.FetchErrorCode_FETCH_ERROR_CODE_CONNECT_FAILURE, v1.FetchErrorCode_FETCH_ERROR_CODE_TLS_FAILURE, v1.FetchErrorCode_FETCH_ERROR_CODE_UPSTREAM_TIMEOUT:
			retry = true
		case v1.FetchErrorCode_FETCH_ERROR_CODE_UPSTREAM_HTTP:
			retry = out.HTTPStatus != nil && retryableFetchStatus(*out.HTTPStatus)
		case v1.FetchErrorCode_FETCH_ERROR_CODE_INVALID_DOCUMENT:
			retry = request.Mode == v1.FetchMode_FETCH_MODE_HTTP_JSON
		}
		out.Retry = retry && attempts+1 < monitorAttemptsTotal
		out.RetryAfter = fullJitterRetryDelay(attempts + 1)
		out.Terminal = !out.Retry
		return out, nil
	}

	for _, m := range monitors {
		for _, rule := range m.Rules {
			item := values[strconv.FormatInt(rule.ID, 10)]
			if e := item.GetError(); e != nil {
				if rule.Source != nil && e.Code == v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_UNSUPPORTED_VALUE {
					out.Terminal = true
					out.Message = "invalid configured source"
				}
				switch e.Code {
				case v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_INVALID_SELECTOR, v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_AMBIGUOUS_SOURCE:
					out.Terminal = true
					out.Message = "invalid selector configuration"
				case v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_EVALUATED:
					return out, errors.New("unexplained unevaluated selector")
				}
				continue
			}
			scalar := item.GetValue()
			if scalar == nil || scalar.Value == nil {
				return out, errors.New("missing scalar")
			}
			if _, boolean := scalar.Value.(*v1.ScalarValue_Boolean); boolean {
				if rule.Source != nil || rule.NumberFormat == "money" {
					out.Terminal = true
					out.Message = "invalid configured number value"
				}
				continue
			}
			raw := scalar.GetText()
			if _, number := scalar.Value.(*v1.ScalarValue_NumberText); number {
				raw = scalar.GetNumberText()
			}
			value, valid := ruleNumericValue(raw, rule.NumberFormat)
			if m.PathType == "selector" || m.PathType == "xpath" {
				value, valid = htmlNumericValue(raw)
			}
			if !valid {
				if rule.NumberFormat == "money" || rule.Source != nil {
					out.Terminal = true
					out.Message = "invalid configured number value"
				}
				continue
			}
			if compare(value, rule.ComparisonOperator, rule.ExpectedValue) {
				out.Matched = append(out.Matched, rule)
			}
		}
		if !out.Terminal && len(out.Matched) >= m.RulesCountToFinish {
			out.Terminal = true
			out.Succeeded = true
			out.Message = "rule threshold reached"
		}
	}
	return out, nil
}
