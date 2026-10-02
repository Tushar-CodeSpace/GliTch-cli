package main

import (
	"testing"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthRepositoryAndHashing(t *testing.T) {
	r := &Repository{
		memUsers: make(map[string]*User),
	}

	// Register user
	user, err := r.CreateUser("cadence", "supersecret123", "Lead Architect")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if user.Username != "cadence" || user.Role != "Lead Architect" {
		t.Fatalf("User attributes mismatch: %+v", user)
	}

	// Verify password hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("supersecret123")); err != nil {
		t.Fatalf("Password verification failed: %v", err)
	}

	// Retrieve user
	fetched, err := r.FindByUsername("cadence")
	if err != nil {
		t.Fatalf("Failed to find user: %v", err)
	}
	if fetched.ID != user.ID {
		t.Fatalf("ID mismatch: got %d, want %d", fetched.ID, user.ID)
	}

	// Duplicate registration prevention
	_, err = r.CreateUser("cadence", "differentpass", "Guest")
	if err == nil {
		t.Fatalf("Expected duplicate user creation to error, got nil")
	}
}

func TestJWTGenerationAndParsing(t *testing.T) {
	user := &User{
		ID:       10,
		Username: "matrix",
		Role:     "Security Admin",
	}

	tokenStr, err := generateJWT(user)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	claims, err := parseJWT(tokenStr)
	if err != nil {
		t.Fatalf("Failed to parse valid JWT: %v", err)
	}

	if claims.Username != "matrix" || claims.UserID != 10 || claims.Role != "Security Admin" {
		t.Fatalf("Claims mismatch: %+v", claims)
	}
}
