package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

// slogRequestLogger is chiMiddleware.Logger's replacement: chi's own
// DefaultLogger is built once at package init time around
// log.New(os.Stdout, ...) - an independent *log.Logger, not the package
// default log.SetDefault/slog bridges - so it never picks up the JSON
// handler installLogging installs; every request line stayed chi's
// colored plain text regardless of KESTRIX_LOG_LEVEL. This formatter
// emits the same information through slog instead, so a request line is a
// JSON object like every other log line - see observability spec.md
// "Log line is machine readable" and design.md D7.
var slogRequestLogger = chiMiddleware.RequestLogger(&slogLogFormatter{})

type slogLogFormatter struct{}

func (slogLogFormatter) NewLogEntry(r *http.Request) chiMiddleware.LogEntry {
	remoteIP := chiMiddleware.GetClientIP(r.Context())
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}
	return &slogLogEntry{
		method:    r.Method,
		route:     r.URL.Path,
		requestID: chiMiddleware.GetReqID(r.Context()),
		remoteIP:  remoteIP,
	}
}

type slogLogEntry struct {
	method    string
	route     string
	requestID string
	remoteIP  string
}

func (e *slogLogEntry) Write(status, bytes int, _ http.Header, elapsed time.Duration, _ interface{}) {
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	} else if status >= 400 {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "request",
		"request_id", e.requestID,
		"method", e.method,
		"route", e.route,
		"status", status,
		"bytes", bytes,
		"duration", elapsed.String(),
		"addr", e.remoteIP,
	)
}

func (e *slogLogEntry) Panic(v interface{}, stack []byte) {
	slog.Error("request panic", "request_id", e.requestID, "route", e.route, "panic", v, "stack", string(stack))
}
