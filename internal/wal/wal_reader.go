// read path: decodes and validates entries from the wal log
// sentinel errors surface corruption to the replay layer
package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

var (
	ErrInvalidMagic     = errors.New("wal: invalid magic number")
	ErrChecksumMismatch = errors.New("wal: entry corrupted")
	ErrShortRead        = errors.New("wal: unexpected EOF reading entry")
)

type Entry struct {
	Op      Opcode
	Version uint8
	Key     string
	Value   string
}

// ReadEntry decodes one entry from r, validates magic and checksum.
func ReadEntry(r io.Reader) (*Entry, error) {
	var hdr [headerSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.EOF //if log is exhausted
		}
		return nil, err
	}

	m := binary.LittleEndian.Uint32(hdr[0:4])
	if m != magic {
		return nil, ErrInvalidMagic //corrupt/partial entries
	}

	kLen := binary.LittleEndian.Uint32(hdr[4:8])
	vLen := binary.LittleEndian.Uint32(hdr[8:12])
	op := Opcode(hdr[12])
	version := hdr[13]
	//hdr[14:16] reserved [IGNORED]

	body := make([]byte, int(kLen)+int(vLen)+4) //+4 for checksum
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, ErrShortRead //corrupt/partial entries
	}

	keyBytes := body[:kLen]
	valBytes := body[kLen : kLen+vLen]
	storedCRC := binary.LittleEndian.Uint32(body[kLen+vLen:])

	//recompute checksum over header[4:16] + key + val.[same bytes written during append]
	h := crc32.NewIEEE()
	h.Write(hdr[4:16])
	h.Write(keyBytes)
	h.Write(valBytes)

	if h.Sum32() != storedCRC {
		return nil, ErrChecksumMismatch //corrupt/partial entries
	}

	return &Entry{
		Op:      op,
		Version: version,
		Key:     string(keyBytes),
		Value:   string(valBytes),
	}, nil
}
