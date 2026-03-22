package server

import (
	"encoding/json"
	"net/http"

	"github.com/samithreddychinni/raftkv/internal/store"
)

type KVData struct {
	Value string `json:"value"`
}

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

func NewServer(store *store.Store) *Server {
	srv := &Server{
		store: store,
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
	s.store.Set(key, data.Value)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	s.store.Delete(key)
	w.WriteHeader(http.StatusNoContent)
}
