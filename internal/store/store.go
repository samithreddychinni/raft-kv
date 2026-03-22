package store

import (
	"sync"
)

type Store struct {
	items map[string]string
	mu    sync.RWMutex
}

func NewStore() *Store {
	return &Store{
		items: make(map[string]string),
	}
}

func (s *Store) GetAll() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copy := make(map[string]string)
	for k, v := range s.items {
		copy[k] = v
	}
	return copy
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RLock()
	val, ok := s.items[key]
	return val, ok
}
func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = value
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}
