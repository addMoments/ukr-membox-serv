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
	// Ev: misafir token'inin bagli oldugu etkinlik (UUID). PostgREST RLS'i
	// current_guest_event_uid() ile okur (3-albums.sql). Host token'inda bos.
	Ev string `json:"ev,omitempty"`
	// Al: misafirin linkten/passcode ile actigi private-protected album UID'leri.
	// PostgREST current_guest_albums() ile okur. Public albumler burada tutulmaz.
	Al []string `json:"al,omitempty"`
}

func (c *TokenClaims) ToMapClaims() *jwt.MapClaims {
	claims := jwt.MapClaims{
		"role": c.Role,
		"ui":   c.UserUID,
		"exp":  c.Exp,
		"ip":   c.IP,
		"iat":  c.Iat,
	}
	if c.Ev != "" {
		claims["ev"] = c.Ev
	}
	if len(c.Al) > 0 {
		claims["al"] = c.Al
	}

	return &claims
}

// HasAlbum, album UID'inin token'da acilmis listede olup olmadigini soyler.
func (c *TokenClaims) HasAlbum(albumUID string) bool {
	for _, a := range c.Al {
		if a == albumUID {
			return true
		}
	}
	return false
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

	// ev / al opsiyonel: eski misafir token'larinda yok, host token'inda hic olmaz.
	if ev, ok := m["ev"].(string); ok {
		c.Ev = ev
	}
	if rawAl, ok := m["al"].([]interface{}); ok {
		for _, item := range rawAl {
			if s, ok := item.(string); ok && s != "" {
				c.Al = append(c.Al, s)
			}
		}
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

	// Ne: Token'in client IP'sine baglanmasi kaldirildi; clientIP parametresi
	//     cagiranlarin imzasini bozmamak icin duruyor, dogrulamada kullanilmiyor.
	// Nasil: claims.IP token'a hala yaziliyor (log/teshis icin), yalnizca
	//        karsilastirma yapilmiyor.
	// Neden: Mobil sebekede (CGNAT) IP gun icinde birkac kez degisiyor; 7 gunluk
	//        token her degisimde gecersiz gorunup kullaniciyi /signin'e dusuruyordu.
	//        Sifresini hatirlamayan musteriler bu yuzden sifre sifirlama dongusune
	//        giriyordu - bir hesapta 1 ayda 10 sifirlama olustu.
	//        db-shell/local-proxy tarafinda ayni kontrol zaten devre disiydi;
	//        iki servis artik tutarli davraniyor.
	return
}
