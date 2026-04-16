// write path: encodes entries and appends them durably to the log file
package wal

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"sync"
)

type WAL struct {
	mu   sync.Mutex
	file *os.File
}

func Open(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: f}, nil
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

func (w *WAL) AppendSet(key, value string) error {
	return w.append(OpSet, key, value)
}

func (w *WAL) AppendDelete(key string) error {
	return w.append(OpDelete, key, "")
}

// append encodes and durably writes one WAL entry.
// header is written as a single 16B buffer; checksum covers header[4:16] + key + val.
func (w *WAL) append(op Opcode, key, value string) error {
	kLen := uint32(len(key))
	vLen := uint32(len(value))

	// build the 16B header; bytes 14-15 are zeroed by make() — reserved
	var hdr [headerSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], kLen)
	binary.LittleEndian.PutUint32(hdr[8:12], vLen)
	hdr[12] = byte(op)
	hdr[13] = currentVersion
	// hdr[14:16] == 0x00 (reserved)

	// checksum over header[4:16] + key + val
	h := crc32.NewIEEE()
	h.Write(hdr[4:16])
	h.Write([]byte(key))
	h.Write([]byte(value))
	checksum := h.Sum32()

	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], checksum)

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.file.Write([]byte(key)); err != nil {
		return err
	}
	if _, err := w.file.Write([]byte(value)); err != nil {
		return err
	}
	if _, err := w.file.Write(crcBuf[:]); err != nil {
		return err
	}
	return w.file.Sync()
}

