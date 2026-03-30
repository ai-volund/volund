package gateway

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/ai-volund/volund/internal/auth"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
// It also implements http.Hijacker so WebSocket upgrades work through
// the middleware chain.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack forwards to the underlying ResponseWriter so websocket.Accept works.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// Flush forwards to the underlying ResponseWriter if it supports it.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// LoggingMiddleware logs one line per request with method, path, status, and duration.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)

		next.ServeHTTP(rw, r)

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", time.Since(start),
			"remote", r.RemoteAddr,
		)
	})
}

// RecoveryMiddleware recovers from panics and returns a 500 error.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"error", rec,
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path,
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware adds CORS headers. Uses VOLUND_CORS_ORIGINS for allowed origins;
// defaults to "*" for development. In production, set this to the actual frontend URLs.
func CORSMiddleware(next http.Handler) http.Handler {
	allowedOrigins := os.Getenv("VOLUND_CORS_ORIGINS") // comma-separated, or "*"
	if allowedOrigins == "" {
		allowedOrigins = "*"
	}
	origins := make(map[string]bool)
	wildcard := false
	if allowedOrigins == "*" {
		wildcard = true
	} else {
		for _, o := range strings.Split(allowedOrigins, ",") {
			origins[strings.TrimSpace(o)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if wildcard {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimitMiddleware implements a simple token bucket rate limiter keyed by
// tenant_id from JWT claims. Limits per-tenant request rate.
func RateLimitMiddleware(requestsPerSecond int, burst int) func(http.Handler) http.Handler {
	if requestsPerSecond <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	type bucket struct {
		tokens    float64
		lastCheck time.Time
	}

	var (
		mu      sync.Mutex
		buckets = make(map[string]*bucket)
		rate    = float64(requestsPerSecond)
		maxB    = float64(burst)
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract tenant from JWT claims if available.
			key := "anon"
			if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims.TenantID != "" {
				key = claims.TenantID
			}

			mu.Lock()
			b, exists := buckets[key]
			if !exists {
				b = &bucket{tokens: maxB, lastCheck: time.Now()}
				buckets[key] = b
			}

			now := time.Now()
			elapsed := now.Sub(b.lastCheck).Seconds()
			b.tokens = min(maxB, b.tokens+elapsed*rate)
			b.lastCheck = now

			if b.tokens < 1 {
				mu.Unlock()
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, `{"error":"rate limit exceeded"}`)
				return
			}
			b.tokens--
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
