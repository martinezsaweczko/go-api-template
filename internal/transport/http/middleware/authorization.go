package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/martinezsaweczko/go-api-template/internal/service"
)

type claimsKey struct{}

// ClaimsFromContext returns the JWT claims validated by AuthorizationMiddleware.
func ClaimsFromContext(ctx context.Context) (*service.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*service.Claims)
	return claims, ok && claims != nil
}

// AuthorizationMiddleware creates a middleware that verifies JWT tokens from the Authorization header
// The token should be in the format: Authorization: Bearer <token>
// Validated claims are stored in the request context; read them with ClaimsFromContext.
func AuthorizationMiddleware(log *slog.Logger, jwtService *service.JWTService) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.WarnContext(ctx, "Missing authorization header", "path", r.URL.Path, "method", r.Method)
				writeJSONError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			scheme, tokenString, ok := strings.Cut(authHeader, " ")
			if !ok || scheme != "Bearer" || tokenString == "" {
				log.WarnContext(ctx, "Invalid authorization header format", "path", r.URL.Path, "method", r.Method)
				writeJSONError(w, http.StatusUnauthorized, "invalid authorization header format")
				return
			}

			claims, err := jwtService.VerifyToken(tokenString)
			if err != nil {
				log.WarnContext(ctx, "Invalid token", "path", r.URL.Path, "method", r.Method, "error", err.Error())
				writeJSONError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			subject, _ := claims.GetSubject()
			log.DebugContext(ctx, "Token verified successfully", "path", r.URL.Path, "method", r.Method, "subject", subject)

			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, claimsKey{}, claims)))
		})
	}
}
