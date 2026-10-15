package handler

import (
	"net/http"
	"strconv"
	"strings"
)

func pathID(w http.ResponseWriter, r *http.Request, prefix string) (int64, bool) {
	rawID := strings.TrimPrefix(r.URL.Path, prefix)
	if rawID == "" || strings.Contains(rawID, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return 0, false
	}

	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}

	return id, true
}
