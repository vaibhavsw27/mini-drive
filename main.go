package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var dbConn *pgx.Conn

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func signupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SignupRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	_, err = dbConn.Exec(context.Background(),
		"INSERT INTO users (email, password_hash) VALUES ($1, $2)",
		req.Email, string(hashedPassword))
	if err != nil {
		http.Error(w, "failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintln(w, "user created successfully")
}

func connectDB() {
	connString := "postgres://minidrive:minidrive123@localhost:5433/minidrive_db"

	conn, err := pgx.Connect(context.Background(), connString)
	if err != nil {
		fmt.Println("Unable to connect to database:", err)
		return
	}
	dbConn = conn
	fmt.Println("Connected to Postgres successfully!")
}

func main() {
	connectDB()

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/signup", signupHandler)

	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
