package auth

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type contextKey string

const claimsKey contextKey = "volund_claims"

// ClaimsFromContext extracts JWT claims from the context.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// UnaryServerInterceptor returns a gRPC unary interceptor that validates JWTs.
func UnaryServerInterceptor(tm *TokenManager) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if shouldSkipAuth(info.FullMethod) {
			return handler(ctx, req)
		}

		claims, err := extractGRPCClaims(ctx, tm)
		if err != nil {
			return nil, err
		}

		ctx = context.WithValue(ctx, claimsKey, claims)
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor that validates JWTs.
func StreamServerInterceptor(tm *TokenManager) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if shouldSkipAuth(info.FullMethod) {
			return handler(srv, ss)
		}

		_, err := extractGRPCClaims(ss.Context(), tm)
		if err != nil {
			return err
		}

		return handler(srv, ss)
	}
}

// HTTPMiddleware returns an HTTP middleware that validates JWTs from the Authorization header.
func HTTPMiddleware(tm *TokenManager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next.ServeHTTP(w, r)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := tm.Validate(tokenStr)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractGRPCClaims(ctx context.Context, tm *TokenManager) (*Claims, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	tokenStr := strings.TrimPrefix(values[0], "Bearer ")
	claims, err := tm.Validate(tokenStr)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	return claims, nil
}

func shouldSkipAuth(method string) bool {
	skipMethods := []string{
		"/grpc.health.v1.Health/",
		"/grpc.reflection.v1alpha.ServerReflection/",
		"/grpc.reflection.v1.ServerReflection/",
		"/volund.v1.AuthService/Login",
	}
	for _, skip := range skipMethods {
		if strings.HasPrefix(method, skip) || method == skip {
			return true
		}
	}
	return false
}
