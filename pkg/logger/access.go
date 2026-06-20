package logger

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// skipAccessLog lists paths that should not produce access log lines.
var skipAccessLog = map[string]bool{
	"/healthz": true,
	"/readyz":  true,
	"/metrics": true,
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// AccessMiddleware logs one structured line per HTTP request.
// It must run inside trace.Middleware so the OTel span is already in context.
//
//	trace.Middleware → AccessMiddleware → metrics.Middleware → mux
//
// Each line includes method, path, status, latency in ms, and trace_id when
// tracing is enabled. Status ≥ 500 is logged at ERROR, 4xx at WARN, rest INFO.
func AccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipAccessLog[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"ms", time.Since(start).Milliseconds(),
		}

		sc := trace.SpanFromContext(r.Context()).SpanContext()
		if sc.IsValid() {
			attrs = append(attrs, "trace_id", sc.TraceID().String())
		}

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "http", attrs...)
	})
}
