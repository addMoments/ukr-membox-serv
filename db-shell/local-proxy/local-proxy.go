package main

import (
	"encoding/json"
	"fmt"
	"local-proxy/src/auth"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/rs/cors"
)

// Config represents the proxy configuration
type Config struct {
	ListenHTTPS bool   `json:"listenHttps"`
	ListenHTTP  bool   `json:"listenHttp"`
	LocalPort   int    `json:"localPort"`
	CertPath    string `json:"certPath"`
	KeyPath     string `json:"keyPath"`
	JwtSecret   string `json:"jwtSecret"`
}

// loadConfig loads the configuration from a JSON file
func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	log.Printf("Loaded config: %+v", config)

	return &config, nil
}

// createReverseProxy creates a reverse proxy to the backend
func createReverseProxy(targetURL *url.URL, config *Config) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Customize the director to add headers like Nginx
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		// Add X-Forwarded headers
		if req.Header.Get("X-Forwarded-For") == "" {
			req.Header.Set("X-Forwarded-For", req.RemoteAddr)
		} else {
			req.Header.Set("X-Forwarded-For", req.Header.Get("X-Forwarded-For")+", "+req.RemoteAddr)
		}

		// Set X-Real-IP
		req.Header.Set("X-Real-IP", req.RemoteAddr)

		// Set X-Forwarded-Proto based on the scheme
		if req.TLS != nil {
			req.Header.Set("X-Forwarded-Proto", "https")
		} else {
			req.Header.Set("X-Forwarded-Proto", "http")
		}

		// Set X-Forwarded-Host
		req.Header.Set("X-Forwarded-Host", req.Host)

		originalDirector(req)
	}

	// Modify response headers
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Add Server header like Nginx
		resp.Header.Set("Server", "membox-db-proxy")

		// Strip CORS headers from upstream (PostgREST) to prevent duplicates
		// Our CORS middleware will add them
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Expose-Headers")
		resp.Header.Del("Access-Control-Allow-Credentials")
		resp.Header.Del("Access-Control-Max-Age")
		return nil
	}

	// Custom error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("502 Bad Gateway"))
	}

	return proxy
}

// authMiddleware validates tokens and sets Authorization header for PostgREST
func authMiddleware(next http.Handler, config *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "authorization header required", http.StatusUnauthorized)
			return
		}

		// Expect "Bearer <token>" format
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]
		if token == "" {
			http.Error(w, "token not found"+authHeader, http.StatusUnauthorized)
			return
		}

		// Validate the token
		clientIP := auth.GetClientIP(r)
		claims, err := auth.ValidateToken(token, clientIP, config.JwtSecret)
		if err != nil {
			log.Printf("Token validation failed: %v", err)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Authorization header already set, just pass through to PostgREST
		log.Printf("Auth OK: role=%s user=%s", claims.Role, claims.UserUID)
		next.ServeHTTP(w, r)
	}
}

func main() {
	// Load configuration
	config, err := loadConfig("local-proxy-config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Parse target URL
	targetURL, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(config.LocalPort))
	if err != nil {
		log.Fatalf("Failed to parse target URL: %v", err)
	}

	log.Printf("Proxying to: %s", targetURL.String())

	// Create reverse proxy
	proxy := createReverseProxy(targetURL, config)

	// HTTP handler with auth middleware
	handlerF := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		proxy.ServeHTTP(w, r)
	}), config)

	/*c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*", "http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:5173", "http://127.0.0.1:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"X-Auth-Token", "Link"},
		AllowCredentials: true,
	})*/
	c := cors.AllowAll()
	handler := c.Handler(handlerF)

	fmt.Println(config)

	// Start HTTP server
	if config.ListenHTTP {
		go func() {
			log.Println("Starting HTTP server on :80")
			if err := http.ListenAndServe(":80", handler); err != nil {
				log.Fatalf("HTTP server failed: %v", err)
			}
		}()
	}

	// Start HTTPS server
	if config.ListenHTTPS {
		log.Println("Starting HTTPS server on :443")
		if err := http.ListenAndServeTLS(":443", config.CertPath, config.KeyPath, handler); err != nil {
			log.Fatalf("HTTPS server failed: %v", err)
		}
	}

	// If both are disabled, exit
	if !config.ListenHTTP && !config.ListenHTTPS {
		log.Fatal("Both HTTP and HTTPS are disabled in config")
	}

	// Block forever
	select {}
}
