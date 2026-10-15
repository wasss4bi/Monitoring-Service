package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"monitoring_service/internal/domain"
	"monitoring_service/internal/service"
)

type idempotencyExecutor struct {
	service *service.IdempotencyService
}

type processingResponseChecker func(context.Context, domain.IdempotencyEntry) (bool, error)
type monitorRecovery func(context.Context, domain.IdempotencyEntry) (int, []byte, error)
type idempotencyContextKey struct{}

func newIdempotencyExecutor(value []*service.IdempotencyService) *idempotencyExecutor {
	if len(value) == 0 || value[0] == nil {
		return nil
	}
	return &idempotencyExecutor{service: value[0]}
}

func (e *idempotencyExecutor) run(w http.ResponseWriter, r *http.Request, userID *int64, next func(http.ResponseWriter, *http.Request)) {
	e.runWithProcessingCheck(w, r, userID, nil, next)
}

func (e *idempotencyExecutor) runWithProcessingCheck(w http.ResponseWriter, r *http.Request, userID *int64, check processingResponseChecker, next func(http.ResponseWriter, *http.Request)) {
	e.runWithRecovery(w, r, userID, check, nil, next)
}

func (e *idempotencyExecutor) runWithRecovery(w http.ResponseWriter, r *http.Request, userID *int64, check processingResponseChecker, recoverMonitor monitorRecovery, next func(http.ResponseWriter, *http.Request)) {
	if e == nil || e.service == nil {
		next(w, r)
		return
	}

	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 255 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required and must contain at most 255 characters")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	entry := domain.IdempotencyEntry{
		Key:         key,
		RequestHash: requestFingerprint(r, body),
		UserID:      userID,
		IP:          requestIP(r),
		Device:      requestDevice(r.Header.Get("User-Agent")),
		Browser:     requestBrowser(r.Header.Get("User-Agent")),
	}
	existing, created, err := e.service.Begin(r.Context(), entry)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			writeError(w, http.StatusBadRequest, service.PublicMessage(err))
			return
		}
		writeServiceError(w, err)
		return
	}
	if !created {
		if existing.RequestHash != entry.RequestHash || !sameIdempotencyUser(existing.UserID, userID) {
			writeError(w, http.StatusConflict, "Idempotency-Key was already used for another request")
			return
		}
		if recoverMonitor != nil && existing.MonitorID != nil {
			code, body, err := recoverMonitor(r.Context(), existing)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeStoredResponse(w, code, body)
			return
		}
		if existing.Status == domain.IdempotencyStatusProcessing {
			if check != nil {
				completed, err := check(r.Context(), existing)
				if err != nil {
					writeServiceError(w, err)
					return
				}
				if completed {
					completeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					err := e.service.Complete(completeCtx, key, existing.ResponseCode, existing.ResponseBody)
					cancel()
					if err != nil {
						setRequestError(w, err)
						writeError(w, http.StatusInternalServerError, "could not confirm the request result; retry with the same Idempotency-Key")
						return
					}
					writeStoredResponse(w, existing.ResponseCode, existing.ResponseBody)
					return
				}
			}
			writeError(w, http.StatusTooEarly, "request with this Idempotency-Key is still processing")
			return
		}
		if existing.Status == domain.IdempotencyStatusCompleted {
			writeStoredResponse(w, existing.ResponseCode, existing.ResponseBody)
			return
		}
		writeError(w, http.StatusInternalServerError, "saved request result is unavailable; retry later with the same Idempotency-Key")
		return
	}

	recorder := newIdempotencyResponseWriter()
	r = r.WithContext(context.WithValue(r.Context(), idempotencyContextKey{}, entry))
	next(recorder, r)
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}

	completeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := e.service.SaveProcessingResponse(completeCtx, key, recorder.status, recorder.body.Bytes()); err != nil {
		setRequestError(w, err)
		writeError(w, http.StatusInternalServerError, "could not confirm the request result; retry with the same Idempotency-Key")
		return
	}
	if err := e.service.Complete(completeCtx, key, recorder.status, recorder.body.Bytes()); err != nil {
		setRequestError(w, err)
		writeError(w, http.StatusInternalServerError, "could not confirm the request result; retry with the same Idempotency-Key")
		return
	}
	recorder.flush(w)
}

func requestFingerprint(r *http.Request, body []byte) string {
	return service.HashRequestBody([]byte(r.Method + "\x00" + r.URL.RequestURI() + "\x00" + service.HashRequestBody(body)))
}

func sameIdempotencyUser(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func writeStoredResponse(w http.ResponseWriter, status int, body []byte) {
	if status == http.StatusNoContent || status == http.StatusNotModified || (status > 0 && status < 200) {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)
	if len(body) == 0 {
		_, _ = w.Write([]byte("null"))
		return
	}
	_, _ = w.Write(body)
}

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func requestDevice(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet"):
		return "tablet"
	case strings.Contains(ua, "mobile") || strings.Contains(ua, "android") || strings.Contains(ua, "iphone"):
		return "mobile"
	default:
		return "desktop"
	}
}

func requestBrowser(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "edg/"):
		return "Edge"
	case strings.Contains(ua, "opr/") || strings.Contains(ua, "opera"):
		return "Opera"
	case strings.Contains(ua, "chrome/"):
		return "Chrome"
	case strings.Contains(ua, "firefox/"):
		return "Firefox"
	case strings.Contains(ua, "safari/"):
		return "Safari"
	default:
		return "Unknown"
	}
}

type idempotencyResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newIdempotencyResponseWriter() *idempotencyResponseWriter {
	return &idempotencyResponseWriter{header: make(http.Header)}
}

func (w *idempotencyResponseWriter) Header() http.Header { return w.header }

func (w *idempotencyResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
}

func (w *idempotencyResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *idempotencyResponseWriter) flush(target http.ResponseWriter) {
	for key, values := range w.header {
		target.Header()[key] = append([]string(nil), values...)
	}
	target.WriteHeader(w.status)
	_, _ = target.Write(w.body.Bytes())
}

var _ http.ResponseWriter = (*idempotencyResponseWriter)(nil)
