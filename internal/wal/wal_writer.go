// write path: encodes entries and appends them durably to the log file
//
// failure safety:
// 1)crash during append i.e, entry is partially written; magic+checksum mismatch detected at replay and entry is skipped.
// 2)crash after wal write but before memory apply i.e, replay re-applies the entry safely (idempotent for SET; harmless re-delete for DELETE).
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
// wire format:- magic(4B), opcode(1B), key_len(4B), val_len(4B), key(key_len B), val(val_len B), crc32(4B)
// the checksum covers everything after the magic: opcode through val.
func (w *WAL) append(op Opcode, key, value string) error {
	kLen := uint32(len(key))
	vLen := uint32(len(value))

	payloadSize := 1 + 4 + 4 + int(kLen) + int(vLen)
	payload := make([]byte, 0, payloadSize)
	payload = append(payload, byte(op))
	payload = binary.LittleEndian.AppendUint32(payload, kLen)
	payload = binary.LittleEndian.AppendUint32(payload, vLen)
	payload = append(payload, key...)
	payload = append(payload, value...)
	checksum := crc32.ChecksumIEEE(payload)

	entry := make([]byte, 0, 4+payloadSize+4)
	entry = binary.LittleEndian.AppendUint32(entry, magic)
	entry = append(entry, payload...)
	entry = binary.LittleEndian.AppendUint32(entry, checksum)

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Write(entry); err != nil {
		return err
	}
	return w.file.Sync()
}
