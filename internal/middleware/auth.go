package middleware

import (
	"context"
	"net/http"
	"strings"

	jwtpkg "ecommerce/server/internal/jwt"
)

type claimsCtxKey struct{}

// RequireAuth validates the Bearer access token and stores its claims in the
// request context for handlers/service to use (user_id, role).
func RequireAuth(jwt *jwtpkg.Helper) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, "Missing or invalid token", "UNAUTHORIZED")
				return
			}

			claims, err := jwt.Validate(token)
			if err != nil {
				if err == jwtpkg.ErrExpired {
					writeError(w, http.StatusUnauthorized, "Token expired", "TOKEN_EXPIRED")
					return
				}
				writeError(w, http.StatusUnauthorized, "Missing or invalid token", "UNAUTHORIZED")
				return
			}

			ctx := context.WithValue(r.Context(), claimsCtxKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole restricts a RequireAuth-wrapped handler to the given role.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFrom(r.Context())
			if !ok {
				writeError(w, http.StatusForbidden, "Forbidden", "FORBIDDEN")
				return
			}
			if claims.Role != role {
				writeError(w, http.StatusForbidden, "Forbidden", "FORBIDDEN")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

// ClaimsFrom returns the authenticated user's claims from the request context.
func ClaimsFrom(ctx context.Context) (*jwtpkg.Claims, bool) {
	claims, ok := ctx.Value(claimsCtxKey{}).(*jwtpkg.Claims)
	return claims, ok
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"success":false,"message":"` + message + `","code":"` + code + `","errors":[]}`))
}
