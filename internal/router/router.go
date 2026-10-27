package router

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"go_study/internal/config"
	"go_study/internal/llm"
	"go_study/pkg/trace"

	"github.com/sony/gobreaker/v2"
)

var httpClient = &http.Client{Timeout: 0}

type Router struct {
	rules       []routeEntry
	globalQuota *llm.TokenQuota
}

type routeEntry struct {
	rule    config.RouteRule
	breaker *gobreaker.CircuitBreaker[*struct{}]
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func NewRouter(rules []config.RouteRule, quota *llm.TokenQuota) (*Router, error) {
	sort.Slice(rules, func(i, j int) bool {
		return len(rules[i].PathPrefix) > len(rules[j].PathPrefix)
	})

	entries := make([]routeEntry, 0, len(rules))
	for _, rule := range rules {
		rule := rule

		// ★ 核心改变：直接调用封装好的函数，代码瞬间清爽！
		breaker := NewCircuitBreaker(rule.PathPrefix)

		entries = append(entries, routeEntry{
			rule:    rule,
			breaker: breaker,
		})
	}
	return &Router{rules: entries, globalQuota: quota}, nil
}

func (r *Router) Match(path string) *routeEntry {
	for i := range r.rules {
		if strings.HasPrefix(path, r.rules[i].rule.PathPrefix) {
			return &r.rules[i]
		}
	}
	return nil
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	entry := r.Match(req.URL.Path)
	if entry == nil {
		http.NotFound(w, req)
		return
	}
	apikey := req.Header.Get("X-API-Key")
	if apikey == "" {
		apikey = req.RemoteAddr
	}
	inputTokensEstimate := 10
	if !r.globalQuota.CheckAndReserve(apikey, inputTokensEstimate) {
		http.Error(w, "Too many requests: token quota exceeded", http.StatusTooManyRequests)
		return
	}
	startTime := time.Now()
	path := req.URL.Path
	if entry.rule.StripPrefix {
		path = strings.TrimPrefix(path, entry.rule.PathPrefix)
		if path == "" {
			path = "/"
		}
	}
	upstreamURL := strings.TrimSuffix(entry.rule.Upstream, "/") + path
	if req.URL.RawQuery != "" {
		upstreamURL += "?" + req.URL.RawQuery
	}

	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusInternalServerError)
		return
	}

	outReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstream.String(), req.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	outReq.Header = req.Header.Clone()

	if traceID := trace.FromContext(req.Context()); traceID != "" {
		outReq.Header.Set("X-Trace-ID", traceID)
	}

	_, err = entry.breaker.Execute(func() (*struct{}, error) {
		resp, err := httpClient.Do(outReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("backend returned status %d", resp.StatusCode)
		}

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)

		counter := llm.NewTokenCounter(resp.Body, apikey, r.globalQuota, startTime)
		buf := make([]byte, 4096)
		for {
			n, readErr := counter.Read(buf)
			if n > 0 {
				_, writeErr := w.Write(buf[:n])
				if writeErr != nil {
					break
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			if readErr != nil {
				if readErr == io.EOF && r.globalQuota.IsExceeded(apikey) {
					slog.Info("stream interrupted due to quota exceeded", "apikey", apikey)
				}
				break
			}
		}
		return nil, nil
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) {
			http.Error(w, "service unavailable (circuit open)", http.StatusServiceUnavailable)
			return
		}
		if rec, ok := w.(*responseRecorder); !ok || rec.status == 0 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}
	}
}
