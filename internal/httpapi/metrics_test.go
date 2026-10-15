package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
