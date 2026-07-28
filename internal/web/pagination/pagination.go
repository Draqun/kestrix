// Package pagination provides reusable pagination parsing and link building for HTTP APIs.
package pagination

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Meta is the JSON-serialisable pagination metadata block.
type Meta struct {
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Next   *string `json:"next"`
	Prev   *string `json:"prev"`
}

// Parse extracts and validates pagination parameters from query values.
// Returns the parsed limit, offset, or an API error string and HTTP status code.
func Parse(q url.Values) (limit int, offset int, errStr string, status int) {
	allowed := map[int]bool{10: true, 25: true, 50: true, 100: true}
	limit = 25
	offset = 0
	status = http.StatusOK
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, 0, `{"error":"invalid limit"}`, http.StatusBadRequest
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, 0, `{"error":"invalid offset"}`, http.StatusBadRequest
		}
		offset = n
	}
	if !allowed[limit] {
		return 0, 0, `{"error":"limit must be one of [10,25,50,100]"}`, http.StatusBadRequest
	}
	return
}

// Links builds next/prev pagination links based on offset, limit, total, and path prefix.
func Links(offset, limit, total int, pathPrefix string) (next, prev *string) {
	end := offset + limit
	if end < total {
		n := fmt.Sprintf("%s?limit=%d&offset=%d", pathPrefix, limit, end)
		next = &n
	}
	if offset > 0 {
		prevOffset := offset - limit
		if prevOffset < 0 {
			prevOffset = 0
		}
		p := fmt.Sprintf("%s?limit=%d&offset=%d", pathPrefix, limit, prevOffset)
		prev = &p
	}
	return
}
