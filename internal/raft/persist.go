//persist: durable Raft state storage.
//
// 2 files live on disk per node:
//
//	<id>.raft.meta  – HardState: currentTerm (uint64) + votedFor (string)
//	<id>.raft.log   – append-only log of LogEntry records
//
// #HardState file format (binary, little-endian)
//
//	[magic:4B][term:8B][vfLen:2B][votedFor:vfLen B][crc32:4B]
//
// magic = 0xRAFTMETA (0x52414654)  detects accidental overwrites / wrong files
// crc32 = IEEE over bytes [4 : 14+vfLen] (term + vfLen + votedFor)
//	
// #Log file format : one record per LogEntry (little-endian)
//
//	[magic:4B][index:8B][term:8B][cmdLen:4B][cmd:cmdLen B][crc32:4B]
//
// magic = 0xRAFTLOGE (0x5241464C)
//crc32 = IEEE over bytes [4 : 24+cmdLen] (index + term + cmdLen + cmd)
//
//Failure safety:
//   - HardState: written to a temp file, fdatasync'd, then renamed atomically.
//     a crash mid-write leaves the old file intact.
//   - Log: partially written tail records have bad magic or bad CRC skipped at
//     replay. the file is then truncated to the last good offset so the writer
//     always appends at a clean boundary.
package raft

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

//magic constants

const (
	metaMagic uint32 = 0x52414654 // "RAFT" in ASCII
	logMagic  uint32 = 0x5241464C // "RAFL" in ASCII
)

//HardState is the minimal persistent Raft state
//every mutation must be written to disk before the RPC reply is sent.
type HardState struct {
	CurrentTerm uint64
	VotedFor    string // "" means no vote cast in this term
}

//persister

//persister owns both on-disk files and exposes SaveHardState / AppendLogEntry
///loadState. all public methods are goroutine-safe with respect to each
//other (each call is internally synchronised by OS-level write ordering and
//fdatasync; the caller still serialises via n.mu).
type Persister struct {
	metaPath string //<dir>/<id>.raft.meta
	logPath  string //<dir>/<id>.raft.log
	logFile  *os.File
}

//NewPersister opens (or creates) the two state files inside dir.
// it does NOT replay them; call LoadState separately.
func NewPersister(dir, id string) (*Persister, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("persist: mkdir %s: %w", dir, err)
	}

	metaPath := filepath.Join(dir, id+".raft.meta")
	logPath := filepath.Join(dir, id+".raft.log")

	//open the log file for append only. we seek to the last good offset
	//during LoadState, so we do NOT use O_TRUNC here.
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("persist: open log %s: %w", logPath, err)
	}

	return &Persister{
		metaPath: metaPath,
		logPath:  logPath,
		logFile:  lf,
	}, nil
}

//Close releases the log file handle.
func (p *Persister) Close() error {
	return p.logFile.Close()
}

//SaveHardState durably overwrites the HardState file using a tmp+rename dance.
//the old state is never exposed to readers mid-write.
func (p *Persister) SaveHardState(hs HardState) error {
	vfBytes := []byte(hs.VotedFor)
	vfLen := uint16(len(vfBytes))

	//total: magic(4) + term(8) + vfLen(2) + votedFor + crc(4)
	totalSize := 4 + 8 + 2 + int(vfLen) + 4
	buf := make([]byte, totalSize)

	binary.LittleEndian.PutUint32(buf[0:4], metaMagic)
	binary.LittleEndian.PutUint64(buf[4:12], hs.CurrentTerm)
	binary.LittleEndian.PutUint16(buf[12:14], vfLen)
	copy(buf[14:], vfBytes)

	//crc over everything after the magic (bytes 4 … end-4)
	checksumStart := 14 + int(vfLen)
	h := crc32.NewIEEE()
	h.Write(buf[4:checksumStart])
	binary.LittleEndian.PutUint32(buf[checksumStart:], h.Sum32())

	//write to a sibling temp file, sync, then rename.
	tmp := p.metaPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("persist: create tmp meta: %w", err)
	}
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return fmt.Errorf("persist: write tmp meta: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("persist: sync tmp meta: %w", err)
	}
	f.Close()
	if err := os.Rename(tmp, p.metaPath); err != nil {
		return fmt.Errorf("persist: rename meta: %w", err)
	}
	return nil
}

//loadHardState reads and validates the HardState file.
//Returns zero-value HardState (term=0, votedFor="") if the file does not exist.
func (p *Persister) loadHardState() (HardState, error) {
	data, err := os.ReadFile(p.metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return HardState{}, nil //first boot
	}
	if err != nil {
		return HardState{}, fmt.Errorf("persist: read meta: %w", err)
	}

	//minimum valid size: 4+8+2+0+4 = 18 bytes (empty votedFor)
	if len(data) < 18 {
		return HardState{}, fmt.Errorf("persist: meta file too short (%d bytes)", len(data))
	}

	if binary.LittleEndian.Uint32(data[0:4]) != metaMagic {
		return HardState{}, fmt.Errorf("persist: meta file has wrong magic")
	}

	term := binary.LittleEndian.Uint64(data[4:12])
	vfLen := binary.LittleEndian.Uint16(data[12:14])

	expectedSize := 18 + int(vfLen)
	if len(data) < expectedSize {
		return HardState{}, fmt.Errorf("persist: meta file truncated")
	}

	vfBytes := data[14 : 14+int(vfLen)]
	storedCRC := binary.LittleEndian.Uint32(data[14+int(vfLen):])

	h := crc32.NewIEEE()
	h.Write(data[4 : 14+int(vfLen)])
	if h.Sum32() != storedCRC {
		return HardState{}, fmt.Errorf("persist: meta CRC mismatch — file may be corrupt")
	}

	return HardState{
		CurrentTerm: term,
		VotedFor:    string(vfBytes),
	}, nil
}

//AppendLogEntry encodes entry and appends it durably to the log file.
//must be called with entries strictly in-order (no gaps).
func (p *Persister) AppendLogEntry(e LogEntry) error {
	cmdLen := uint32(len(e.Command))

	//magic(4) + index(8) + term(8) + cmdLen(4) + cmd + crc(4)
	totalSize := 4 + 8 + 8 + 4 + int(cmdLen) + 4
	buf := make([]byte, totalSize)

	binary.LittleEndian.PutUint32(buf[0:4], logMagic)
	binary.LittleEndian.PutUint64(buf[4:12], e.Index)
	binary.LittleEndian.PutUint64(buf[12:20], e.Term)
	binary.LittleEndian.PutUint32(buf[20:24], cmdLen)
	copy(buf[24:], e.Command)

	checksumStart := 24 + int(cmdLen)
	h := crc32.NewIEEE()
	h.Write(buf[4:checksumStart])
	binary.LittleEndian.PutUint32(buf[checksumStart:], h.Sum32())

	if _, err := p.logFile.Write(buf); err != nil {
		return fmt.Errorf("persist: write log entry: %w", err)
	}
	if err := p.logFile.Sync(); err != nil {
		return fmt.Errorf("persist: sync log: %w", err)
	}
	return nil
}

// TruncateLogFrom removes all log records with index >= fromIndex by finding
//the file offset of fromIndex and truncating the file there.
func (p *Persister) TruncateLogFrom(fromIndex uint64) error {
	if _, err := p.logFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var offset int64
	for {
		startOffset, err := p.logFile.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		var magic [4]byte
		if _, err := io.ReadFull(p.logFile, magic[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}

		if binary.LittleEndian.Uint32(magic[:]) != logMagic {
			break //corrupt tail; stop here
		}

		var header [20]byte //index(8) + term(8) + cmdLen(4)
		if _, err := io.ReadFull(p.logFile, header[:]); err != nil {
			break
		}
		idx := binary.LittleEndian.Uint64(header[0:8])
		cmdLen := binary.LittleEndian.Uint32(header[16:20])

		// skip cmd + crc
		if _, err := p.logFile.Seek(int64(cmdLen)+4, io.SeekCurrent); err != nil {
			break
		}

		if idx >= fromIndex {
			// this record and everything after must go
			offset = startOffset
			break
		}
		offset = 0 // will be set to startOffset of next record
		_ = startOffset
	}

	if offset == 0 {
		// fromIndex is beyond the end of file — nothing to truncate
		return nil
	}

	if err := p.logFile.Truncate(offset); err != nil {
		return fmt.Errorf("persist: truncate log at offset %d: %w", offset, err)
	}
	if _, err := p.logFile.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("persist: seek to end after truncate: %w", err)
	}
	return nil
}

//RecoveredState is returned by LoadState on node restart.
type RecoveredState struct {
	HardState HardState
	Log       []LogEntry //excludes the sentinel; caller prepends it
}

//LoadState reads both files, validates every record, and returns the
//recovered Raft state.  Corrupt tail bytes are silently discarded and the
//log file is truncated to the last clean record so future appends land on a
//valid boundary.
func (p *Persister) LoadState() (RecoveredState, error) {
	hs, err := p.loadHardState()
	if err != nil {
		return RecoveredState{}, err
	}

	entries, lastGoodOffset, err := p.replayLog()
	if err != nil {
		return RecoveredState{}, err
	}

	// Truncate corrupt tail (if any) and reposition write cursor.
	info, statErr := p.logFile.Stat()
	if statErr != nil {
		return RecoveredState{}, fmt.Errorf("persist: stat log: %w", statErr)
	}
	if info.Size() != lastGoodOffset {
		if err := p.logFile.Truncate(lastGoodOffset); err != nil {
			return RecoveredState{}, fmt.Errorf("persist: truncate corrupt tail: %w", err)
		}
	}
	if _, err := p.logFile.Seek(lastGoodOffset, io.SeekStart); err != nil {
		return RecoveredState{}, fmt.Errorf("persist: seek to write cursor: %w", err)
	}

	return RecoveredState{HardState: hs, Log: entries}, nil
}

// replayLog scans the log file from the beginning, decoding and validating
// every record.  Returns the valid entries and the file offset of the last
// clean byte (== file position after the last good record).
func (p *Persister) replayLog() ([]LogEntry, int64, error) {
	if _, err := p.logFile.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}

	var entries []LogEntry
	var lastGoodOffset int64

	for {
		var magicBuf [4]byte
		if _, err := io.ReadFull(p.logFile, magicBuf[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, lastGoodOffset, err
		}
		if binary.LittleEndian.Uint32(magicBuf[:]) != logMagic {
			break // corrupt or partial write then stop here
		}

		var header [20]byte // index(8) + term(8) + cmdLen(4)
		if _, err := io.ReadFull(p.logFile, header[:]); err != nil {
			break
		}
		idx := binary.LittleEndian.Uint64(header[0:8])
		term := binary.LittleEndian.Uint64(header[8:16])
		cmdLen := binary.LittleEndian.Uint32(header[16:20])

		payload := make([]byte, int(cmdLen)+4) // cmd + crc
		if _, err := io.ReadFull(p.logFile, payload); err != nil {
			break
		}
		cmd := payload[:cmdLen]
		storedCRC := binary.LittleEndian.Uint32(payload[cmdLen:])

		// Validate CRC over [index..cmdLen+cmd]
		h := crc32.NewIEEE()
		h.Write(header[:])
		h.Write(cmd)
		if h.Sum32() != storedCRC {
			break // checksum mismatch discard this and the rest
		}

		entries = append(entries, LogEntry{Index: idx, Term: term, Command: cmd})

		pos, err := p.logFile.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, lastGoodOffset, err
		}
		lastGoodOffset = pos
	}

	return entries, lastGoodOffset, nil
}
