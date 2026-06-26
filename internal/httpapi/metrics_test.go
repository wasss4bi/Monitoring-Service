package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogRequestSkipsBadRequestAndLogsServerError(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantLog bool
	}{{http.StatusBadRequest, false}, {http.StatusInternalServerError, true}} {
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		h := logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}), logger)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		if got := output.Len() > 0; got != tc.wantLog {
			t.Errorf("status %d: logged=%t, want %t", tc.status, got, tc.wantLog)
		}
	}
}

func TestLogRequestLogsBadRequestWithInternalError(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	h := logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setRequestError(w, errors.New("body read failed"))
		w.WriteHeader(http.StatusBadRequest)
	}), logger)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	if output.Len() == 0 {
		t.Fatal("internal error was not logged")
	}
}

func TestStatusRecorderDefaultsToOK(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}

	if status := recorder.Status(); status != http.StatusOK {
		t.Errorf("Status() = %d, want %d", status, http.StatusOK)
	}
}

func TestStatusRecorderCapturesWrittenStatus(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	recorder.WriteHeader(http.StatusCreated)

	if status := recorder.Status(); status != http.StatusCreated {
		t.Errorf("Status() = %d, want %d", status, http.StatusCreated)
	}
}

func TestStatusRecorderCapturesRequestError(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	want := errors.New("upstream unavailable")

	setRequestError(recorder, want)

	if !errors.Is(recorder.RequestError(), want) {
		t.Errorf("RequestError() = %v, want %v", recorder.RequestError(), want)
	}
}
