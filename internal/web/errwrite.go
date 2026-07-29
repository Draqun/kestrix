package web

import (
	"encoding/json"
	"net/http"
)

// writeError sends a JSON error response with the given status code.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
