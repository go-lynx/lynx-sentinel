package sentinel

import (
	"context"
	"fmt"
	"net/http"

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
func (s *PlugSentinel) CreateHTTPMiddleware(resourceExtractor func(interface{}) string) interface{} {
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

			// Execute the next handler
			defer func() {
				if entry != nil {
					// Exit the entry (this would be done by the actual Sentinel entry)
					// For now, we'll just log it
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// CreateGRPCInterceptor creates gRPC interceptor for Sentinel protection
// This method returns a middleware instance that provides both unary and stream interceptors
func (s *PlugSentinel) CreateGRPCInterceptor() interface{} {
	// Return the SentinelMiddleware which provides GRPCUnaryInterceptor and GRPCStreamInterceptor
	return s.CreateMiddleware()
}
