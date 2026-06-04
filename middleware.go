package sentinel

import (
	"context"
	"fmt"
	"net/http"

	"github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/base"
	kratosmiddleware "github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
)

func (s *PlugSentinel) rateLimitMiddleware(defaultResource string) kratosmiddleware.Middleware {
	return func(next kratosmiddleware.Handler) kratosmiddleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			resource := defaultResource
			if tr, ok := transport.FromServerContext(ctx); ok && tr != nil && tr.Operation() != "" {
				resource = tr.Operation()
			}

			var resp any
			err := s.ExecuteWithContext(ctx, resource, func(runCtx context.Context) error {
				var err error
				resp, err = next(runCtx, req)
				return err
			})
			return resp, err
		}
	}
}

// HTTPRateLimit implements lynx.RateLimiter for HTTP entry points.
func (s *PlugSentinel) HTTPRateLimit() kratosmiddleware.Middleware {
	return s.rateLimitMiddleware("http.request")
}

// GRPCRateLimit implements lynx.RateLimiter for gRPC entry points.
func (s *PlugSentinel) GRPCRateLimit() kratosmiddleware.Middleware {
	return s.rateLimitMiddleware("grpc.request")
}

// CreateHTTPMiddleware creates HTTP middleware for Sentinel protection
func (s *PlugSentinel) CreateHTTPMiddleware(resourceExtractor func(any) string) any {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resource := resourceExtractor(r)
			if resource == "" {
				resource = r.URL.Path
			}

			entry, err := s.Entry(resource)
			if err != nil {
				// Request blocked by Sentinel
				http.Error(w, fmt.Sprintf("Request blocked: %v", err), http.StatusTooManyRequests)
				return
			}

			// Always exit the Sentinel entry once the handler completes, otherwise
			// concurrency/statistic counters never decrement and flow/breaker rules
			// eventually block all traffic.
			sentinelEntry, ok := entry.(*base.SentinelEntry)
			if ok && sentinelEntry != nil {
				defer sentinelEntry.Exit()
			}

			// Capture the response status so 5xx responses are reported to Sentinel
			// for circuit-breaker accounting.
			sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			if ok && sentinelEntry != nil && sw.status >= http.StatusInternalServerError {
				api.TraceError(sentinelEntry, fmt.Errorf("handler returned status %d", sw.status))
			}
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the response status code
// so the Sentinel HTTP middleware can report server errors to the circuit breaker.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	if !sr.wroteHeader {
		sr.status = code
		sr.wroteHeader = true
	}
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wroteHeader {
		sr.wroteHeader = true
	}
	return sr.ResponseWriter.Write(b)
}

// CreateGRPCInterceptor creates gRPC interceptor for Sentinel protection.
// This method returns a middleware instance that provides both unary and stream interceptors.
func (s *PlugSentinel) CreateGRPCInterceptor() any {
	// Return the SentinelMiddleware which provides GRPCUnaryInterceptor and GRPCStreamInterceptor
	return s.CreateMiddleware()
}
