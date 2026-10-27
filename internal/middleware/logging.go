package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"go_study/pkg/trace"
)

func LoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = trace.NewTraceID()
		}

		ctx := trace.WithTraceID(r.Context(), traceID)
		r = r.WithContext(ctx)

		reqLogger := logger.With("trace_id", traceID)
		start := time.Now()

		reqLogger.Info("request started", "method", r.Method, "path", r.URL.Path)

		rec := &statusRecorder{ResponseWriter: w, status: 0}
		next.ServeHTTP(rec, r)

		reqLogger.Info("request finished",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status != 0 {
		r.ResponseWriter.WriteHeader(code)
		return
	}
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
