package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/samithreddychinni/raftkv/internal/raft"
	"github.com/samithreddychinni/raftkv/internal/wal"
)

type Store struct {
	items map[string]string
	mu    sync.RWMutex
	log   *wal.WAL
}

// NewStoreFromWAL opens the wal file at path, replays all valid entries into
// memory, truncates any torn write at the end, then returns a store that is
// ready to accept live traffic.
func NewStoreFromWAL(path string) (*Store, error) {
	w, err := wal.Open(path)
	if err != nil {
		return nil, fmt.Errorf("store: open wal: %w", err)
	}

	s := &Store{
		items: make(map[string]string),
		log:   w,
	}

	if err := s.replay(path); err != nil {
		return nil, fmt.Errorf("store: replay: %w", err)
	}

	return s, nil
}

// replay reads the wal file from the beginning, applying every valid entry.
// it stops and truncates at the first occurance of corruption.
func (s *Store) replay(path string) error {
	f, err := openReadOnly(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var lastGoodOffset int64

	for {
		entry, err := wal.ReadEntry(f)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break //clean end of log
			}
			//corruption detected: truncate everything after the last good entry
			if truncErr := truncate(path, lastGoodOffset); truncErr != nil {
				return fmt.Errorf("truncate after corruption: %w", truncErr)
			}
			break
		}

		//apply valid entry directly to the map (bypassing WAL — replay only)
		switch entry.Op {
		case wal.OpSet:
			s.items[entry.Key] = entry.Value
		case wal.OpDelete:
			delete(s.items, entry.Key)
		}

		pos, err := currentOffset(f)
		if err != nil {
			return err
		}
		lastGoodOffset = pos
	}

	return nil
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
	defer s.mu.RUnlock()
	val, ok := s.items[key]
	return val, ok
}

func (s *Store) Set(key, value string) error {
	if err := s.log.AppendSet(key, value); err != nil { //WAL write first
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = value
	return nil
}

func (s *Store) Delete(key string) error {
	if err := s.log.AppendDelete(key); err != nil { //WAL write first
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	return nil
}

func (s *Store) Close() error {
	return s.log.Close()
}

// cmd is the command encoded inside a raft.LogEntry.Command
// the HTTP handler encodes this apply decodes it
type Cmd struct {
	Op    string `json:"op"`    //"set"or"delete"
	Key   string `json:"key"`
	Value string `json:"value"` //empty for delete
}

// apply decodes a committed raft log entry and writes it to the store
// called by the apply loop in main.go same code path for leader and follower
func (s *Store) Apply(entry raft.LogEntry) error {
	var cmd Cmd
	if err := json.Unmarshal(entry.Command, &cmd); err != nil {
		return fmt.Errorf("store: decode command: %w", err)
	}
	switch cmd.Op {
	case "set":
		return s.Set(cmd.Key, cmd.Value)
	case "delete":
		return s.Delete(cmd.Key)
	default:
		return fmt.Errorf("store: unknown op %q", cmd.Op)
	}
}
