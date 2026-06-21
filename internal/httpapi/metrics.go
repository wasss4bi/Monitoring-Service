package httpapi

import (
	"net/http"
	"time"

	"monitoring_service/internal/metrics"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	err    error
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) Status() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func (r *statusRecorder) SetRequestError(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *statusRecorder) RequestError() error {
	return r.err
}

func setRequestError(w http.ResponseWriter, err error) {
	if recorder, ok := w.(interface{ SetRequestError(error) }); ok {
		recorder.SetRequestError(err)
	}
}

func observeHTTPRequest(method, route, status string, duration time.Duration) {
	metrics.ObserveHTTPRequest(method, route, status, duration)
}
