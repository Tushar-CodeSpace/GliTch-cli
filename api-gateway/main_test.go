package main

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(10.0, 5)

	// First 5 requests should pass burst limit
	for i := 0; i < 5; i++ {
		if !rl.Allow("127.0.0.1") {
			t.Fatalf("Expected request %d to be allowed under burst limit", i+1)
		}
	}

	// 6th immediate request should be throttled
	if rl.Allow("127.0.0.1") {
		t.Fatalf("Expected immediate 6th request to be denied by rate limiter")
	}

	// A different IP address should have its own separate bucket
	if !rl.Allow("192.168.1.100") {
		t.Fatalf("Expected different IP to be allowed")
	}
}

func TestPublicRoutes(t *testing.T) {
	gw := &Gateway{}

	publicCases := []struct {
		path   string
		method string
		want   bool
	}{
		{"/api/v1/auth/login", "POST", true},
		{"/api/v1/login", "POST", true},
		{"/api/v1/auth/register", "POST", true},
		{"/health", "GET", true},
		{"/api/v1/cluster/health", "GET", true},
		{"/api/v1/telemetry/stats", "OPTIONS", true},
		{"/api/v1/telemetry/stats", "GET", false},
		{"/api/v1/auth/user", "GET", false},
	}

	for _, tc := range publicCases {
		got := gw.isPublicRoute(tc.path, tc.method)
		if got != tc.want {
			t.Errorf("isPublicRoute(%s, %s) = %v; want %v", tc.path, tc.method, got, tc.want)
		}
	}
}

func TestValidateJWT(t *testing.T) {
	secret := []byte("test-secret-key-1234567890123456")
	gw := &Gateway{
		cfg: Config{JWTSecret: secret},
	}

	claims := Claims{
		UserID:   42,
		Username: "agent-ops",
		Role:     "Operator",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			Subject:   "agent-ops",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	parsed, err := gw.validateJWT("Bearer " + tokenStr)
	if err != nil {
		t.Fatalf("Failed to validate JWT: %v", err)
	}
	if parsed.Username != "agent-ops" || parsed.UserID != 42 || parsed.Role != "Operator" {
		t.Fatalf("Parsed claims mismatch: %+v", parsed)
	}

	// Test invalid token
	_, err = gw.validateJWT("Bearer invalid-token-string")
	if err == nil {
		t.Fatalf("Expected error for invalid token, got nil")
	}
}
