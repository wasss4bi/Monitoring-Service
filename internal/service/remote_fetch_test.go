package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"monitoring_service/internal/domain"
	"monitoring_service/internal/repository"
)

type preparationLimiter struct {
	waits     []time.Duration
	calls     int
	onAcquire func()
}

func (l *preparationLimiter) AcquireKey(ctx context.Context, _ string, _ int, _ time.Duration) (time.Duration, error) {
	l.calls++
	if l.onAcquire != nil {
		l.onAcquire()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return l.waits[l.calls-1], nil
}

func TestPreparationPermitWaitsWithinAttemptBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Second)
	defer cancel()
	limiter := &preparationLimiter{waits: []time.Duration{time.Millisecond, 0}}
	s := &MonitoringExecutionService{limiter: limiter}
	wait, err := s.preparationPermit(ctx, domain.FetchJob{TargetHostnameID: 7, TargetLimitPerMin: 1})
	if err != nil || wait != 0 || limiter.calls != 2 {
		t.Fatalf("wait=%v calls=%d err=%v", wait, limiter.calls, err)
	}
}

func TestPreparationPermitKeepsQuotaAndBudget(t *testing.T) {
	for _, remaining := range []time.Duration{10 * time.Second, 200 * time.Second} {
		t.Run(remaining.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), remaining)
			defer cancel()
			limiter := &preparationLimiter{waits: []time.Duration{time.Millisecond, time.Minute}}
			s := &MonitoringExecutionService{limiter: limiter}
			wait, err := s.preparationPermit(ctx, domain.FetchJob{})
			calls, expected := 1, time.Millisecond
			if remaining > sessionPreparationRPCLimit {
				calls, expected = 2, time.Minute
			}
			if err != nil || wait != expected || limiter.calls != calls {
				t.Fatalf("wait=%v calls=%d err=%v", wait, limiter.calls, err)
			}
		})
	}
}

func TestPreparationPermitCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Second)
	defer cancel()
	limiter := &preparationLimiter{waits: []time.Duration{time.Millisecond}, onAcquire: cancel}
	s := &MonitoringExecutionService{limiter: limiter}
	_, err := s.preparationPermit(ctx, domain.FetchJob{})
	if !errors.Is(err, context.Canceled) || limiter.calls != 1 {
		t.Fatalf("calls=%d err=%v", limiter.calls, err)
	}
}

func remoteFixture() (*v1.FetchRequest, []repository.EvaluationMonitor) {
	m := []repository.EvaluationMonitor{{ID: 1, PathType: "json", RulesCountToFinish: 1, Rules: []domain.MonitorRule{{ID: 1, JSONPath: "a", ComparisonOperator: "<", ExpectedValue: 10}, {ID: 2, JSONPath: "b", ComparisonOperator: "<", ExpectedValue: 10}}}}
	r := makeExtractionRequest(domain.FetchJob{CanonicalURL: "https://example.com/value", EndpointType: "json"}, m)
	return r, m
}
func TestRemoteThresholdMissingAndInvalidSelector(t *testing.T) {
	r, monitors := remoteFixture()
	for _, code := range []v1.ExtractionErrorCode{v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_FOUND, v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_INVALID_SELECTOR} {
		response := &v1.FetchResponse{RequestId: r.RequestId, Results: []*v1.SelectorResult{{Id: "1", Outcome: &v1.SelectorResult_Value{Value: &v1.ScalarValue{Value: &v1.ScalarValue_Text{Text: "1"}}}}, {Id: "2", Outcome: &v1.SelectorResult_Error{Error: &v1.ExtractionError{Code: code}}}}}
		o, err := evaluateRemoteResponse(r, response, monitors, 0, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !o.Terminal || o.Succeeded != (code == v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_FOUND) {
			t.Fatal(o)
		}
	}
}
func TestRemoteGeneralFailureSuppressesCachedTrigger(t *testing.T) {
	r, m := remoteFixture()
	n := uint32(503)
	response := &v1.FetchResponse{RequestId: r.RequestId, Failure: &v1.FetchError{Code: v1.FetchErrorCode_FETCH_ERROR_CODE_UPSTREAM_HTTP, HttpStatus: &n}, Results: []*v1.SelectorResult{{Id: "1", Origin: v1.ValueOrigin_VALUE_ORIGIN_CACHE, Outcome: &v1.SelectorResult_Value{Value: &v1.ScalarValue{Value: &v1.ScalarValue_Text{Text: "1"}}}}}}
	response.Results = append(response.Results, &v1.SelectorResult{Id: "2", Outcome: &v1.SelectorResult_Error{Error: &v1.ExtractionError{Code: v1.ExtractionErrorCode_EXTRACTION_ERROR_CODE_NOT_EVALUATED}}})
	o, err := evaluateRemoteResponse(r, response, m, 0, time.Minute)
	if err != nil || o.Succeeded || !o.Retry || len(o.Matched) > 0 {
		t.Fatal(o, err)
	}
	o, err = evaluateRemoteResponse(r, response, m, 5, time.Minute)
	if err != nil || o.Retry || !o.Terminal {
		t.Fatal(o, err)
	}
}
func TestRemoteMalformedResultsAreInfrastructureErrors(t *testing.T) {
	r, m := remoteFixture()
	for _, response := range []*v1.FetchResponse{{RequestId: "wrong"}, {RequestId: r.RequestId, Results: []*v1.SelectorResult{{Id: "1"}, {Id: "1"}}}} {
		if _, err := evaluateRemoteResponse(r, response, m, 0, time.Minute); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
}
func TestHTMLNumericParity(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		value float64
		valid bool
	}{{"1 234,50 ₽", 1234.5, true}, {"$1,234.56", 1234.56, true}, {"1.234,56", 1234.56, true}, {"- 1", 0, false}, {"1.234", 0, false}, {"1 234.567", 1234567, true}, {"1234.567", 1234.567, true}, {"NaN", 0, false}, {"1e3", 0, false}, {"1,234,567", 0, false}, {"", 0, false}, {"€ 0", 0, true}} {
		t.Run(strconv.Quote(tc.raw), func(t *testing.T) {
			v, ok := htmlNumericValue(tc.raw)
			if ok != tc.valid || v != tc.value {
				t.Fatalf("%v %v", v, ok)
			}
		})
	}
}
