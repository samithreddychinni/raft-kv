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
	Op    Opcode
	Key   string
	Value string
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

	op := Opcode(hdr[4])
	kLen := binary.LittleEndian.Uint32(hdr[5:9])
	vLen := binary.LittleEndian.Uint32(hdr[9:13])

	body := make([]byte, int(kLen)+int(vLen)+4) //+4 for checksum
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, ErrShortRead
	}

	keyBytes := body[:kLen]
	valBytes := body[kLen : kLen+vLen]
	storedCRC := binary.LittleEndian.Uint32(body[kLen+vLen:])

	//recompute checksum over payload.[same bytes written during append]
	h := crc32.NewIEEE()
	h.Write([]byte{byte(op)})
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], kLen)
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:], vLen)
	h.Write(buf[:])
	h.Write(keyBytes)
	h.Write(valBytes)

	if h.Sum32() != storedCRC {
		return nil, ErrChecksumMismatch //corrupt/partial entries
	}

	return &Entry{
		Op:    op,
		Key:   string(keyBytes),
		Value: string(valBytes),
	}, nil
}
