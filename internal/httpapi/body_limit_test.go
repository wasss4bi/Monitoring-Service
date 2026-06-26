package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestBodyLimits(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /one", func(w http.ResponseWriter, r *http.Request) { _, _ = io.ReadAll(r.Body); w.WriteHeader(204) })
	mux.HandleFunc("POST /two", func(w http.ResponseWriter, r *http.Request) { _, _ = io.ReadAll(r.Body); w.WriteHeader(204) })
	app := withBodyLimits(mux, map[string]int64{"POST /two": 3})
	for _, tc := range []struct {
		path    string
		n       int
		chunked bool
		want    int
	}{
		{"/one", 1 << 20, false, 204}, {"/one", (1 << 20) + 1, false, 413}, {"/one", (1 << 20) + 1, true, 413}, {"/two", 3, true, 204}, {"/two", 4, true, 413},
	} {
		t.Run(tc.path+"/"+strconv.Itoa(tc.n), func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(strings.Repeat("x", tc.n)))
			if tc.chunked {
				r.ContentLength = -1
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
			if w.Code == 413 {
				var v any
				if json.Unmarshal(w.Body.Bytes(), &v) != nil {
					t.Fatal("invalid error JSON")
				}
			}
		})
	}
}
