package auth

import (
	"errors"
	"membox-serv/src/env"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenLife = 7 * 24 * time.Hour

type TokenClaims struct {
	Role    string `json:"role"`
	UserUID string `json:"ui"`
	Exp     int64  `json:"exp"`
	IP      string `json:"ip"`
	Iat     int64  `json:"iat"`
}

func (c *TokenClaims) ToMapClaims() *jwt.MapClaims {
	claims := jwt.MapClaims{
		"role": c.Role,
		"ui":   c.UserUID,
		"exp":  c.Exp,
		"ip":   c.IP,
		"iat":  c.Iat,
	}

	return &claims
}

func (c *TokenClaims) FromMapClaims(m jwt.MapClaims) (err error) {
	defer (func() {
		r := recover()
		if r != nil {
			err = errors.New("invalid token")
			return
		}
	})()

	exp, ok := m["exp"].(float64)
	if !ok {
		err = errors.New("invalid token")
		return
	}

	iat, ok := m["iat"].(float64)
	if !ok {
		err = errors.New("invalid token")
		return
	}

	*c = TokenClaims{
		Role:    m["role"].(string),
		UserUID: m["ui"].(string),
		Exp:     int64(exp),
		IP:      m["ip"].(string),
		Iat:     int64(iat),
	}

	return
}

// GenerateToken creates a new JWT token with the given claims
func (c *TokenClaims) GenerateToken(key string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c.ToMapClaims())
	return token.SignedString([]byte(key))
}

// SetToken sets the authorization token in the response for client storage
func SetToken(w http.ResponseWriter, token string) {
	w.Header().Set("X-Auth-Token", token)
}

// GetToken extracts the authorization token from the request
// Returns the token string or an error if not found/invalid format
func GetToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("authorization header required")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", errors.New("invalid authorization header format")
	}

	token := parts[1]
	if token == "" {
		return "", errors.New("token not found")
	}

	return token, nil
}

// GetClientIP extracts the real client IP from the request
func GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For first (set by reverse proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	parts := strings.Split(r.RemoteAddr, ":")
	return parts[0]
}

func ValidateToken(encToken, clientIP string) (claims TokenClaims, err error) {
	mapClaims := jwt.MapClaims{}

	// Parse and validate JWT
	token, err := jwt.ParseWithClaims(encToken, &mapClaims, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(env.Env().Jwt_secret), nil
	})
	if err != nil {
		return
	}

	// Check if token is valid
	if !token.Valid {
		err = errors.New("invalid token")
		return
	}

	err = claims.FromMapClaims(mapClaims)
	if err != nil {
		return
	}

	if claims.Role == "auth" && (claims.IP != "" && claims.IP != clientIP) {
		err = errors.New("ip address mismatch")
		return
	}
	return
}
