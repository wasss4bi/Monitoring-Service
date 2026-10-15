package service
import "testing"
func TestHTTPStatusRetryClassification(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		if !retryableFetchStatus(status) {
			t.Errorf("HTTP %d should retry", status)
		}
	}
	for _, status := range []int{400, 401, 403, 404, 405, 409, 413, 422, 498} {
		if retryableFetchStatus(status) {
			t.Errorf("HTTP %d should terminate", status)
		}
	}
}
func TestAllComparisonOperatorsAtBoundary(t *testing.T) {
	for _, tc := range []struct {
		operator string
		want     bool
	}{
		{"<", false}, {"<=", true}, {"=", true}, {"!=", false}, {">=", true}, {">", false},
	} {
		if got := compare(400, tc.operator, 400); got != tc.want {
			t.Errorf("400 %s 400 = %v, want %v", tc.operator, got, tc.want)
		}
	}
}
