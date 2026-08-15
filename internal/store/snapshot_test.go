package store

import (
	"path/filepath"
	"testing"
)

func TestRestoreSnapshot_ReplacesWALForRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.wal")
	s, err := NewStoreFromWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("old", "value"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreSnapshot([]byte(`{"snapshot":"value"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("later", "value"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewStoreFromWAL(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, ok := restarted.Get("old"); ok {
		t.Fatal("pre-snapshot key survived restart")
	}
	if got, ok := restarted.Get("snapshot"); !ok || got != "value" {
		t.Fatalf("snapshot key = %q, %t; want value, true", got, ok)
	}
	if got, ok := restarted.Get("later"); !ok || got != "value" {
		t.Fatalf("post-snapshot key = %q, %t; want value, true", got, ok)
	}
}
