package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config holds gateway settings.
type Config struct {
	Port                string
	AuthServiceURL      string
	TelemetryServiceURL string
	RedisHost           string
	JWTSecret           []byte
	RateLimitRPS        float64
	RateLimitBurst      int
}

// TokenBucket implements a simple, concurrent token-bucket rate limiter.
type TokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
	rps     float64
	burst   int
}

func newRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*TokenBucket),
		rps:     rps,
		burst:   burst,
	}

	// Periodic cleanup of stale client buckets every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for k, b := range rl.buckets {
				if now.Sub(b.lastRefill) > 10*time.Minute {
					delete(rl.buckets, k)
				}
			}
			rl.mu.Unlock()
		}
	}()

	return rl
}

func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[clientIP]
	if !exists {
		b = &TokenBucket{
			tokens:     float64(rl.burst),
			maxTokens:  float64(rl.burst),
			refillRate: rl.rps,
			lastRefill: now,
		}
		rl.buckets[clientIP] = b
	}

	// Refill tokens based on elapsed duration
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// Claims defines custom JWT claims.
type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type Gateway struct {
	cfg            Config
	rateLimiter    *RateLimiter
	authProxy      *httputil.ReverseProxy
	telemetryProxy *httputil.ReverseProxy
}

func parseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		log.Fatalf("Invalid upstream URL %s: %v", raw, err)
	}
	return u
}

func newProxy(target *url.URL, stripPrefix, addPrefix string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	origDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = target.Host

		// Normalize paths
		if stripPrefix != "" && strings.HasPrefix(req.URL.Path, stripPrefix) {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, stripPrefix)
			if !strings.HasPrefix(req.URL.Path, "/") {
				req.URL.Path = "/" + req.URL.Path
			}
		}
		if addPrefix != "" {
			req.URL.Path = addPrefix + req.URL.Path
		}
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Printf("Proxy error for %s %s -> %s: %v", req.Method, req.URL.Path, target.String(), err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "Bad Gateway",
			"message": "Downstream microservice unavailable",
			"target":  target.String(),
			"path":    req.URL.Path,
		})
	}

	return proxy
}

func (gw *Gateway) isPublicRoute(path, method string) bool {
	if method == http.MethodOptions {
		return true
	}
	publicPaths := []string{
		"/api/v1/auth/login",
		"/api/v1/login",
		"/api/v1/auth/register",
		"/api/v1/register",
		"/health",
		"/api/v1/health",
		"/api/v1/cluster/health",
	}
	for _, p := range publicPaths {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func (gw *Gateway) validateJWT(tokenStr string) (*Claims, error) {
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return gw.cfg.JWTSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token claims")
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// ClusterHealth aggregates health across microservices.
type ServiceHealth struct {
	Service string `json:"service"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	Latency string `json:"latency"`
	Details any    `json:"details,omitempty"`
}

type ClusterHealthResponse struct {
	Status    string          `json:"status"`
	Gateway   string          `json:"gateway"`
	Timestamp string          `json:"timestamp"`
	Services  []ServiceHealth `json:"services"`
}

func checkService(name, url string) ServiceHealth {
	start := time.Now()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	latency := time.Since(start).String()

	if err != nil {
		return ServiceHealth{
			Service: name,
			URL:     url,
			Status:  "Degraded / Offline",
			Latency: latency,
			Details: err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var details any
		_ = json.NewDecoder(resp.Body).Decode(&details)
		return ServiceHealth{
			Service: name,
			URL:     url,
			Status:  "Healthy (Online)",
			Latency: latency,
			Details: details,
		}
	}

	return ServiceHealth{
		Service: name,
		URL:     url,
		Status:  fmt.Sprintf("HTTP %d", resp.StatusCode),
		Latency: latency,
	}
}

func (gw *Gateway) handleClusterHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	authHealthURL := gw.cfg.AuthServiceURL + "/health"
	telemetryHealthURL := gw.cfg.TelemetryServiceURL + "/health"

	services := []ServiceHealth{
		checkService("auth-service", authHealthURL),
		checkService("telemetry-service", telemetryHealthURL),
	}

	// Check Redis ping if configured
	if gw.cfg.RedisHost != "" {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", gw.cfg.RedisHost, 2*time.Second)
		lat := time.Since(start).String()
		if err == nil {
			conn.Close()
			services = append(services, ServiceHealth{
				Service: "redis-cache",
				URL:     gw.cfg.RedisHost,
				Status:  "Healthy (Online)",
				Latency: lat,
			})
		} else {
			services = append(services, ServiceHealth{
				Service: "redis-cache",
				URL:     gw.cfg.RedisHost,
				Status:  "Offline",
				Latency: lat,
				Details: err.Error(),
			})
		}
	}

	allHealthy := true
	for _, s := range services {
		if !strings.Contains(s.Status, "Healthy") {
			allHealthy = false
			break
		}
	}

	status := "Healthy"
	if !allHealthy {
		status = "Degraded"
	}

	res := ClusterHealthResponse{
		Status:    status,
		Gateway:   "Online (Port " + gw.cfg.Port + ")",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Services:  services,
	}
	json.NewEncoder(w).Encode(res)
}

func (gw *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS handling
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	clientIP := getClientIP(r)

	// 1. Rate Limiting
	if !gw.rateLimiter.Allow(clientIP) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error":               "Too Many Requests",
			"message":             "Rate limit exceeded. Please back off.",
			"retry_after_seconds": 1,
		})
		log.Printf("[RATE_LIMIT] Client IP %s exceeded limit on %s %s", clientIP, r.Method, r.URL.Path)
		return
	}

	// 2. Gateway Direct Endpoints
	if r.URL.Path == "/health" || r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/cluster/health" {
		gw.handleClusterHealth(w, r)
		return
	}

	// 3. JWT Authentication Verification for Protected Routes
	var claims *Claims
	if !gw.isPublicRoute(r.URL.Path, r.Method) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Unauthorized",
				"message": "Authorization header missing. Please provide a valid Bearer token.",
			})
			return
		}

		c, err := gw.validateJWT(authHeader)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Unauthorized",
				"message": "Invalid or expired token: " + err.Error(),
			})
			return
		}
		claims = c

		// Inject user claims into request headers for downstream microservices
		r.Header.Set("X-User-ID", strconv.FormatInt(claims.UserID, 10))
		r.Header.Set("X-User-Name", claims.Username)
		r.Header.Set("X-User-Role", claims.Role)
	}

	// 4. Reverse Proxy Routing
	path := r.URL.Path
	start := time.Now()
	userTag := "anonymous"
	if claims != nil {
		userTag = claims.Username
	}

	switch {
	case strings.HasPrefix(path, "/api/v1/auth/"):
		gw.authProxy.ServeHTTP(w, r)
	case path == "/api/v1/login" || path == "/api/v1/register" || path == "/api/v1/user" || path == "/api/v1/verify":
		gw.authProxy.ServeHTTP(w, r)
	case strings.HasPrefix(path, "/api/v1/telemetry/"):
		gw.telemetryProxy.ServeHTTP(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Not Found",
			"message": fmt.Sprintf("No route matched for %s %s", r.Method, path),
		})
	}

	duration := time.Since(start)
	log.Printf("[GATEWAY] %s %s | IP: %s | User: %s | Latency: %v", r.Method, path, clientIP, userTag, duration)
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func main() {
	port := getEnv("PORT", "8000")
	authURL := getEnv("AUTH_SERVICE_URL", "http://localhost:8081")
	telemetryURL := getEnv("TELEMETRY_SERVICE_URL", "http://localhost:8082")
	redisHost := getEnv("REDIS_HOST", "localhost:6379")
	secret := getEnv("JWT_SECRET", "glitch-ultra-secure-monorepo-secret-key-32b")

	rps, _ := strconv.ParseFloat(getEnv("RATE_LIMIT_RPS", "30.0"), 64)
	burst, _ := strconv.Atoi(getEnv("RATE_LIMIT_BURST", "60"))

	cfg := Config{
		Port:                port,
		AuthServiceURL:      authURL,
		TelemetryServiceURL: telemetryURL,
		RedisHost:           redisHost,
		JWTSecret:           []byte(secret),
		RateLimitRPS:        rps,
		RateLimitBurst:      burst,
	}

	log.Printf("Initializing GliTch API Gateway on port :%s", cfg.Port)
	log.Printf("Upstream Auth Service:      %s", cfg.AuthServiceURL)
	log.Printf("Upstream Telemetry Service: %s", cfg.TelemetryServiceURL)

	gw := &Gateway{
		cfg:            cfg,
		rateLimiter:    newRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		authProxy:      newProxy(parseURL(cfg.AuthServiceURL), "", ""),
		telemetryProxy: newProxy(parseURL(cfg.TelemetryServiceURL), "", ""),
	}

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      gw,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // 0 for streaming SSE support
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("⚡ GliTch API Gateway proxying traffic on :%s...", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Gateway server error: %v", err)
	}
}
