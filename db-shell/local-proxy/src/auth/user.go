package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AuthMiddleware is a unified middleware for both users and guests.
// IP validation is role-based (only enforced for "auth" role in ValidateToken).
func AuthMiddleware(next http.HandlerFunc, jwtSecret string) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		var stat_code int
		var claims TokenClaims

		defer (func() {
			if err != nil {
				if stat_code == 0 {
					stat_code = 500
				}
				http.Error(w, err.Error(), stat_code)
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), "claims", claims)))
		})()

		ip := GetClientIP(r)

		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			err = errors.New("authorization header required")
			stat_code = 401
			return
		}

		// Expect "Bearer <token>" format
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			err = errors.New("invalid authorization header format")
			stat_code = 401
			return
		}

		encToken := parts[1]
		if encToken == "" {
			err = errors.New("token not found")
			stat_code = 401
			return
		}

		claims, err = ValidateToken(encToken, ip, jwtSecret)
		if err != nil {
			stat_code = 401
			return
		}
	})
}

// Authorize creates a JWT token for the given role and user.
// - role: "auth" for authenticated users, "webanon" for anonymous guests
// - userUID: user identifier (if empty, a new UUID is generated for guests)
// Returns the JWT token that client should store and send via Authorization: Bearer header
func Authorize(w http.ResponseWriter, r *http.Request, role string, userUID string, jwtSecret string) (claims TokenClaims, token string, err error) {
	// Generate userUID for guests if not provided
	if userUID == "" {
		if role == "auth" {
			err = errors.New("userUID is required for authenticated users")
			return
		}
		userUID = uuid.New().String()
	}

	// IP validation only applies to "auth" role
	ip := "-"
	if role == "auth" {
		ip = GetClientIP(r)
	}

	now := time.Now()
	claims = TokenClaims{
		Role:    role,
		UserUID: userUID,
		IP:      ip,
		Exp:     now.Add(tokenLife).Unix(),
		Iat:     now.Unix(),
	}

	fmt.Println("authorize", role, claims)

	token, err = claims.GenerateToken(jwtSecret)
	if err != nil {
		return
	}

	// Set token in response header for client to capture and store
	w.Header().Set("X-Auth-Token", token)

	r = r.WithContext(context.WithValue(r.Context(), "claims", claims))
	return
}
