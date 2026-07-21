package main

import (
	"fmt"
	"net/http"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func main() {
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
