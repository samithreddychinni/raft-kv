package main

import (
	"fmt"
	"net/http"

	"github.com/samithreddychinni/raftkv/internal/store"
	"github.com/samithreddychinni/raftkv/server"
)

func main() {
	s := store.NewStore()
	srv := server.NewServer(s)
	fmt.Println("RaftKV Server starting on :8080...")
	if err := http.ListenAndServe(":8080", srv); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
	}
}
