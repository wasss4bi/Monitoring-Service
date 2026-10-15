package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/service"
)

type inMemoryIdempotencyRepository struct {
	entries map[string]domain.IdempotencyEntry
}

func (r *inMemoryIdempotencyRepository) CreateProcessing(_ context.Context, entry domain.IdempotencyEntry) (bool, error) {
	if r.entries == nil {
		r.entries = make(map[string]domain.IdempotencyEntry)
	}
	if existing, ok := r.entries[entry.Key]; ok && existing.ExpiresAt.After(time.Now()) {
		return false, nil
	}
	r.entries[entry.Key] = entry
	return true, nil
}

func (r *inMemoryIdempotencyRepository) Get(_ context.Context, key string) (domain.IdempotencyEntry, error) {
	entry, ok := r.entries[key]
	if !ok {
		return domain.IdempotencyEntry{}, errors.New("not found")
	}
	return entry, nil
}

func (r *inMemoryIdempotencyRepository) MarkProcessingResponse(_ context.Context, key string, code int, body []byte) error {
	entry, ok := r.entries[key]
	if !ok {
		return errors.New("not found")
	}
	entry.ResponseCode = code
	entry.ResponseBody = append([]byte(nil), body...)
	r.entries[key] = entry
	return nil
}

func (r *inMemoryIdempotencyRepository) MarkCompleted(_ context.Context, key string, code int, body []byte) error {
	entry, ok := r.entries[key]
	if !ok {
		return errors.New("not found")
	}
	entry.Status = domain.IdempotencyStatusCompleted
	entry.ResponseCode = code
	entry.ResponseBody = append([]byte(nil), body...)
	r.entries[key] = entry
	return nil
}

func TestIdempotencyExecutorReplaysCompletedResponse(t *testing.T) {
	repository := &inMemoryIdempotencyRepository{}
	executor := &idempotencyExecutor{service: service.NewIdempotencyService(repository, time.Hour)}
	calls := 0
	next := func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeJSON(w, http.StatusCreated, map[string]string{"result": "created"})
	}

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Alice"}`))
	firstRequest.Header.Set("Idempotency-Key", "key-1")
	executor.run(first, firstRequest, nil, next)

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Alice"}`))
	secondRequest.Header.Set("Idempotency-Key", "key-1")
	executor.run(second, secondRequest, nil, next)

	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d and %d, want two 201 responses", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replayed body = %q, want %q", second.Body.String(), first.Body.String())
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestIdempotencyExecutorReplaysNoContentWithoutBody(t *testing.T) {
	repository := &inMemoryIdempotencyRepository{}
	executor := &idempotencyExecutor{service: service.NewIdempotencyService(repository, time.Hour)}
	calls := 0
	for range 2 {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/me/monitors/1/resume", nil)
		request.Header.Set("Idempotency-Key", "resume-empty-response")
		executor.run(response, request, nil, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusNoContent)
		})
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("resume response: HTTP %d body=%q", response.Code, response.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("handler calls=%d, want 1", calls)
	}
}

func TestIdempotencyExecutorRejectsDifferentBody(t *testing.T) {
	repository := &inMemoryIdempotencyRepository{}
	executor := &idempotencyExecutor{service: service.NewIdempotencyService(repository, time.Hour)}
	next := func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) }

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"value":1}`))
	firstRequest.Header.Set("Idempotency-Key", "key-1")
	executor.run(first, firstRequest, nil, next)

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"value":2}`))
	secondRequest.Header.Set("Idempotency-Key", "key-1")
	executor.run(second, secondRequest, nil, next)

	if second.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", second.Code, http.StatusConflict)
	}
}

func TestIdempotencyExecutorReturnsTooEarlyForProcessingRequest(t *testing.T) {
	repository := &inMemoryIdempotencyRepository{entries: map[string]domain.IdempotencyEntry{
		"key-1": {Key: "key-1", Status: domain.IdempotencyStatusProcessing, RequestHash: requestFingerprint(httptest.NewRequest(http.MethodPost, "/users", nil), []byte(`{"value":1}`)), ExpiresAt: time.Now().Add(time.Hour)},
	}}
	executor := &idempotencyExecutor{service: service.NewIdempotencyService(repository, time.Hour)}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"value":1}`))
	request.Header.Set("Idempotency-Key", "key-1")
	executor.run(recorder, request, nil, func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not be called") })

	if recorder.Code != http.StatusTooEarly {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooEarly)
	}
}

func TestIdempotencyExecutorCompletesStoredResponseWhenProcessingResourceIsCompleted(t *testing.T) {
	repository := &inMemoryIdempotencyRepository{entries: map[string]domain.IdempotencyEntry{
		"key-1": {
			Key:          "key-1",
			Status:       domain.IdempotencyStatusProcessing,
			RequestHash:  requestFingerprint(httptest.NewRequest(http.MethodPost, "/me/monitors", nil), []byte(`{"value":1}`)),
			ResponseCode: http.StatusAccepted,
			ResponseBody: []byte(`{"id":7}`),
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}}
	executor := &idempotencyExecutor{service: service.NewIdempotencyService(repository, time.Hour)}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/me/monitors", strings.NewReader(`{"value":1}`))
	request.Header.Set("Idempotency-Key", "key-1")

	executor.runWithProcessingCheck(recorder, request, nil, func(_ context.Context, entry domain.IdempotencyEntry) (bool, error) {
		return string(entry.ResponseBody) == `{"id":7}`, nil
	}, func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not be called")
	})

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if recorder.Body.String() != `{"id":7}` {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), `{"id":7}`)
	}
	if repository.entries["key-1"].Status != domain.IdempotencyStatusCompleted {
		t.Fatalf("status = %q, want completed", repository.entries["key-1"].Status)
	}
}
