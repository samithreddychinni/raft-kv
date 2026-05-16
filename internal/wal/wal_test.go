package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
)

//helpers
func openTemp(t *testing.T) (*WAL, string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "wal-*.log")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.Close()
	w, err := Open(f.Name())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, f.Name()
}

//append + read round-trip
func TestAppendSet_RoundTrip(t *testing.T) {
	w, path := openTemp(t)

	if err := w.AppendSet("hello", "world"); err != nil {
		t.Fatalf("AppendSet: %v", err)
	}
	w.Close()

	f, _ := os.Open(path)
	defer f.Close()

	entry, err := ReadEntry(f)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if entry.Op != OpSet {
		t.Errorf("op = %v; want OpSet", entry.Op)
	}
	if entry.Key != "hello" || entry.Value != "world" {
		t.Errorf("got (%q, %q); want (hello, world)", entry.Key, entry.Value)
	}

	_, err = ReadEntry(f) //no more entries
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestAppendDelete_RoundTrip(t *testing.T) {
	w, path := openTemp(t)
	if err := w.AppendDelete("gone"); err != nil {
		t.Fatalf("AppendDelete: %v", err)
	}
	w.Close()

	f, _ := os.Open(path)
	defer f.Close()

	entry, err := ReadEntry(f)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if entry.Op != OpDelete {
		t.Errorf("op = %v; want OpDelete", entry.Op)
	}
	if entry.Key != "gone" || entry.Value != "" {
		t.Errorf("got (%q, %q); want (gone, )", entry.Key, entry.Value)
	}
}

func TestMultipleEntries(t *testing.T) {
	w, path := openTemp(t)
	ops := []struct {
		op Opcode
		k  string
		v  string
		fn func() error
	}{
		{OpSet, "a", "1", func() error { return w.AppendSet("a", "1") }},
		{OpSet, "b", "2", func() error { return w.AppendSet("b", "2") }},
		{OpDelete, "a", "", func() error { return w.AppendDelete("a") }},
	}
	for _, o := range ops {
		if err := o.fn(); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	w.Close()

	f, _ := os.Open(path)
	defer f.Close()
	for i, want := range ops {
		e, err := ReadEntry(f)
		if err != nil {
			t.Fatalf("[%d] ReadEntry: %v", i, err)
		}
		if e.Op != want.op || e.Key != want.k || e.Value != want.v {
			t.Errorf("[%d] got (%v,%q,%q); want (%v,%q,%q)", i, e.Op, e.Key, e.Value, want.op, want.k, want.v)
		}
	}
}

//corruption detection
func TestReadEntry_CorruptChecksum(t *testing.T) {
	w, path := openTemp(t)
	_ = w.AppendSet("key", "val")
	w.Close()

	f, _ := os.OpenFile(path, os.O_RDWR, 0o644)
	defer f.Close()
	info, _ := f.Stat()
	f.WriteAt([]byte{0xFF}, info.Size()-1) //flip the last byte[checksum]
	f.Seek(0, io.SeekStart)

	_, err := ReadEntry(f)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestReadEntry_BadMagic(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(0xCAFEBABE)) //wrong magic
	buf.Write(make([]byte, 28))                                  //remaining header+body padding (16-4 + some body)

	_, err := ReadEntry(&buf)
	if !errors.Is(err, ErrInvalidMagic) {
		t.Errorf("expected ErrInvalidMagic, got %v", err)
	}
}

func TestReadEntry_EmptyFile(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadEntry(&buf)
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF on empty reader, got %v", err)
	}
}

func TestReadEntry_TruncatedBody(t *testing.T) {
	// write a valid 16B header with key_len=5, val_len=5 at correct aligned offsets; omit body
	var hdr [headerSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], 5)  //key_len = 5
	binary.LittleEndian.PutUint32(hdr[8:12], 5) //val_len = 5
	hdr[12] = byte(OpSet)
	hdr[13] = currentVersion
	//body is missing entirely

	_, err := ReadEntry(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrShortRead) {
		t.Errorf("expected ErrShortRead, got %v", err)
	}
}

//empty key/value edge cases
func TestAppendSet_EmptyValue(t *testing.T) {
	w, path := openTemp(t)
	_ = w.AppendSet("emptyval", "")
	w.Close()

	f, _ := os.Open(path)
	defer f.Close()
	e, err := ReadEntry(f)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if e.Key != "emptyval" || e.Value != "" {
		t.Errorf("got (%q,%q)", e.Key, e.Value)
	}
}
