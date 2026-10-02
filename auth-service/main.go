package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// User represents an authenticated identity in GliTch.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Credentials holds login and registration payloads.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

// AuthResponse represents the authentication payload returned to clients.
type AuthResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
	User    *User  `json:"user,omitempty"`
}

// Claims defines custom JWT claims.
type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Repository handles user persistence across PostgreSQL or an in-memory fallback.
type Repository struct {
	db         *sql.DB
	memUsers   map[string]*User
	mu         sync.RWMutex
	isPostgres bool
}

var (
	jwtSecret = []byte(getEnv("JWT_SECRET", "glitch-ultra-secure-monorepo-secret-key-32b"))
	repo      *Repository
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func initRepo() *Repository {
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "glitch_user")
	dbPass := getEnv("DB_PASSWORD", "glitch_pass")
	dbName := getEnv("DB_NAME", "glitch_db")
	dbSSL := getEnv("DB_SSLMODE", "disable")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s connect_timeout=3",
		dbHost, dbPort, dbUser, dbPass, dbName, dbSSL)

	db, err := sql.Open("postgres", connStr)
	isPG := false
	if err == nil && db.Ping() == nil {
		log.Printf("Connected to PostgreSQL database at %s:%s/%s", dbHost, dbPort, dbName)
		isPG = true
		setupSchema(db)
	} else {
		log.Printf("PostgreSQL not immediately reachable (%v). Initializing in-memory fallback storage.", err)
	}

	r := &Repository{
		db:         db,
		memUsers:   make(map[string]*User),
		isPostgres: isPG,
	}

	// Always ensure default admin user 'snyder' exists with password 'glitch123'
	defaultHash, _ := bcrypt.GenerateFromPassword([]byte("glitch123"), bcrypt.DefaultCost)
	defaultUser := &User{
		ID:           1,
		Username:     "snyder",
		PasswordHash: string(defaultHash),
		Role:         "DevOps Engineer",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if isPG {
		var exists bool
		_ = db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)", "snyder").Scan(&exists)
		if !exists {
			_, err = db.Exec("INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)",
				defaultUser.Username, defaultUser.PasswordHash, defaultUser.Role)
			if err != nil {
				log.Printf("Failed to seed default user in Postgres: %v", err)
			} else {
				log.Println("Seeded default admin user 'snyder' into PostgreSQL.")
			}
		}
	} else {
		r.memUsers["snyder"] = defaultUser
	}

	return r
}

func setupSchema(db *sql.DB) {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(64) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		role VARCHAR(64) NOT NULL DEFAULT 'DevOps Engineer',
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS login_audits (
		id SERIAL PRIMARY KEY,
		username VARCHAR(64) NOT NULL,
		ip_address VARCHAR(45) NOT NULL,
		success BOOLEAN NOT NULL,
		timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(query); err != nil {
		log.Printf("Warning: Failed to execute schema migration: %v", err)
	} else {
		log.Println("Database schema verified and up to date.")
	}
}

func (r *Repository) FindByUsername(username string) (*User, error) {
	if r.isPostgres && r.db != nil {
		row := r.db.QueryRow("SELECT id, username, password_hash, role, created_at, updated_at FROM users WHERE username = $1", username)
		u := &User{}
		err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
		if err == nil {
			return u, nil
		}
		if err != sql.ErrNoRows {
			log.Printf("PostgreSQL query error: %v, falling back to memory", err)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.memUsers[username]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return u, nil
}

func (r *Repository) CreateUser(username, password, role string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if role == "" {
		role = "DevOps Engineer"
	}

	now := time.Now().UTC()
	if r.isPostgres && r.db != nil {
		var id int64
		err := r.db.QueryRow(
			"INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5) RETURNING id",
			username, string(hash), role, now, now,
		).Scan(&id)
		if err == nil {
			return &User{
				ID:           id,
				Username:     username,
				PasswordHash: string(hash),
				Role:         role,
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		}
		log.Printf("PostgreSQL insert failed: %v", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.memUsers[username]; exists {
		return nil, fmt.Errorf("user already exists")
	}
	user := &User{
		ID:           int64(len(r.memUsers) + 1),
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	r.memUsers[username] = user
	return user, nil
}

func (r *Repository) RecordAudit(username, ip string, success bool) {
	if r.isPostgres && r.db != nil {
		_, _ = r.db.Exec("INSERT INTO login_audits (username, ip_address, success) VALUES ($1, $2, $3)", username, ip, success)
	}
}

func generateJWT(u *User) (string, error) {
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "glitch-auth-service",
			Subject:   u.Username,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func parseJWT(tokenStr string) (*Claims, error) {
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token claims")
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AuthResponse{Success: false, Message: "Invalid request payload"})
		return
	}

	ip := r.RemoteAddr
	user, err := repo.FindByUsername(creds.Username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)) != nil {
		repo.RecordAudit(creds.Username, ip, false)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(AuthResponse{Success: false, Message: "Invalid username or password"})
		return
	}

	token, err := generateJWT(user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(AuthResponse{Success: false, Message: "Failed to generate security token"})
		return
	}

	repo.RecordAudit(creds.Username, ip, true)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(AuthResponse{
		Success: true,
		Token:   token,
		Message: "Authentication successful!",
		User:    user,
	})
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil || creds.Username == "" || creds.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AuthResponse{Success: false, Message: "Username and password required"})
		return
	}

	user, err := repo.CreateUser(creds.Username, creds.Password, creds.Role)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(AuthResponse{Success: false, Message: err.Error()})
		return
	}

	token, _ := generateJWT(user)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AuthResponse{
		Success: true,
		Token:   token,
		Message: "User registered successfully!",
		User:    user,
	})
}

func handleUserProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized: Missing authorization header"})
		return
	}

	claims, err := parseJWT(authHeader)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized: " + err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"username": claims.Username,
		"role":     claims.Role,
		"user_id":  claims.UserID,
		"system":   "GliTch-cli v1.0",
		"status":   "Active",
		"expires":  claims.ExpiresAt.Time.Format(time.RFC3339),
	})
}

func handleVerifyToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		authHeader = r.URL.Query().Get("token")
	}
	if authHeader == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"valid": false, "message": "Token parameter missing"})
		return
	}

	claims, err := parseJWT(authHeader)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"valid": false, "message": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"valid":    true,
		"username": claims.Username,
		"role":     claims.Role,
		"user_id":  claims.UserID,
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dbStatus := "in-memory-fallback"
	if repo.isPostgres && repo.db != nil && repo.db.Ping() == nil {
		dbStatus = "connected (PostgreSQL)"
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "healthy",
		"service":  "auth-service",
		"database": dbStatus,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func generateRandomSecret() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func main() {
	repo = initRepo()

	mux := http.NewServeMux()

	// Supported auth routes (support both direct /api/v1/auth/* and legacy /api/v1/*)
	mux.HandleFunc("POST /api/v1/auth/login", handleLogin)
	mux.HandleFunc("POST /api/v1/login", handleLogin)

	mux.HandleFunc("POST /api/v1/auth/register", handleRegister)
	mux.HandleFunc("POST /api/v1/register", handleRegister)

	mux.HandleFunc("GET /api/v1/auth/user", handleUserProfile)
	mux.HandleFunc("GET /api/v1/user", handleUserProfile)

	mux.HandleFunc("GET /api/v1/auth/verify", handleVerifyToken)
	mux.HandleFunc("GET /api/v1/verify", handleVerifyToken)

	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /api/v1/auth/health", handleHealth)

	port := getEnv("PORT", "8081")
	log.Printf("⚡ GliTch Auth Microservice running on port %s...", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server shutdown: %v", err)
	}
}