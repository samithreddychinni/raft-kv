package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/samithreddychinni/raftkv/internal/store"
	"github.com/samithreddychinni/raftkv/server"
)

const walPath = "raftkvd.wal"

func main() {
	s, err := store.NewStoreFromWAL(walPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	srv := server.NewServer(s)
	fmt.Println("RaftKV Server starting on :8080...")
	if err := http.ListenAndServe(":8080", srv); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
	}
}
