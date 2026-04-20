package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/samithreddychinni/raftkv/internal/raft"
	"github.com/samithreddychinni/raftkv/internal/store"
)

type KVData struct {
	Value string `json:"value"`
}

type Server struct {
	store *store.Store
	raft  *raft.RaftNode
	mux   *http.ServeMux
}

func NewServer(s *store.Store, rn *raft.RaftNode) *Server {
	srv := &Server{
		store: s,
		raft:  rn,
		mux:   http.NewServeMux(),
	}
	srv.routes()
	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("GET /key/{key}", s.handleGet)
	s.mux.HandleFunc("POST /key/{key}", s.handleSet)
	s.mux.HandleFunc("DELETE /key/{key}", s.handleDelete)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.store.GetAll())
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	value, ok := s.store.Get(key)
	if !ok {
		http.Error(w, "didnt found key", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var data KVData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !s.raft.IsLeader() {
		w.Header().Set("X-Raft-Leader", s.raft.LeaderAddr())
		http.Error(w, "not the leader", http.StatusServiceUnavailable)
		return
	}
	cmd, _ := json.Marshal(store.Cmd{Op: "set", Key: key, Value: data.Value})
	if err := s.raft.Propose(cmd); err != nil {
		http.Error(w, fmt.Sprintf("propose failed: %v", err), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !s.raft.IsLeader() {
		w.Header().Set("X-Raft-Leader", s.raft.LeaderAddr())
		http.Error(w, "not the leader", http.StatusServiceUnavailable)
		return
	}
	cmd, _ := json.Marshal(store.Cmd{Op: "delete", Key: key})
	if err := s.raft.Propose(cmd); err != nil {
		http.Error(w, fmt.Sprintf("propose failed: %v", err), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

