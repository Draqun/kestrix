package web

import (
	"encoding/json"
	"errors"
	"net/http"
)

// writeError sends a JSON error response with the given status code.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeJSON decodes r.Body into v, answering 413 when the body exceeded the
// route's configured bound (see maxBody) and 400 on any other decode
// failure. It reports whether decoding succeeded; on false the caller must
// return without running its business logic - the response has already been
// written. See design.md D5.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}
