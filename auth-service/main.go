package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
}

func main() {
	mux := http.NewServeMux()

	// Login endpoint
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var creds Credentials
		err := json.NewDecoder(r.Body).Decode(&creds)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(AuthResponse{Success: false, Message: "Invalid request body"})
			return
		}

		// Simple mock validation (replace with DB check later)
		if creds.Username == "snyder" && creds.Password == "glitch123" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(AuthResponse{
				Success: true,
				Token:   "glitch_sec_token_998877",
				Message: "Login successful!",
			})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(AuthResponse{
				Success: false,
				Message: "Invalid username or password",
			})
		}
	})

	// Protected user profile endpoint
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		
		// Simple mock token check
		if token == "Bearer glitch_sec_token_998877" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"username": "snyder",
				"role":     "DevOps Engineer",
				"system":   "GliTch-cli v1.0",
				"status":   "Active",
			})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized: Invalid or missing token"})
		}
	})

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Auth service is running..."))
	})

	log.Println("Auth microservice running on port 8080...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}