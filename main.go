package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
)

var dbConn *pgx.Conn

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
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

	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
