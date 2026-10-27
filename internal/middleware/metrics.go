package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"go_study/internal/metrics"
)

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)

		duration := time.Since(start).Seconds()
		status := "200"
		if rec, ok := w.(*statusRecorder); ok {
			status = fmt.Sprintf("%d", rec.status)
		}
		pathLabel := normalizePath(r.URL.Path)
		metrics.HttpRequestsTotal.WithLabelValues(r.Method, pathLabel, status).Inc()
		metrics.HttpRequestDuration.WithLabelValues(r.Method, pathLabel).Observe(duration)
	})
}

func normalizePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if isNumeric(seg) {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
