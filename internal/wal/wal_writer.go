// write path: encodes entries and appends them durably to the log file
package wal

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"sync"
)

type LogRequest struct {
	Data []byte
	Resp chan error
}

type WAL struct {
	mu     sync.Mutex
	file   *os.File
	reqCh  chan LogRequest
	closed bool
}

func Open(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := &WAL{
		file:  f,
		reqCh: make(chan LogRequest, 1024),
	}
	go w.runGroupCommit()
	return w, nil
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	close(w.reqCh)
	return w.file.Close()
}

func (w *WAL) AppendSet(key, value string) error {
	return w.append(OpSet, key, value)
}

func (w *WAL) AppendDelete(key string) error {
	return w.append(OpDelete, key, "")
}

// append encodes the entry into a single contiguous buffer and 
// sends it to the background group commit loop for durable writing.
func (w *WAL) append(op Opcode, key, value string) error {
	kLen := uint32(len(key))
	vLen := uint32(len(value))

	// 1. Calculate the exact size of the entire log entry
	// 16B header + Key length + Value length + 4B Checksum
	totalSize := headerSize + int(kLen) + int(vLen) + 4
	
	// 2. Allocate a single contiguous buffer for the whole entry
	buf := make([]byte, totalSize)

	// 3. Write the header directly into the buffer
	binary.LittleEndian.PutUint32(buf[0:4], magic)
	binary.LittleEndian.PutUint32(buf[4:8], kLen)
	binary.LittleEndian.PutUint32(buf[8:12], vLen)
	buf[12] = byte(op)
	buf[13] = currentVersion
	// buf[14:16] are implicitly zeroed by make()

	// 4. Copy the key and value payloads into the buffer
	payloadStart := headerSize
	copy(buf[payloadStart:], key)
	copy(buf[payloadStart+int(kLen):], value)

	// 5. Calculate and append the CRC32 checksum
	// Note: We checksum buf[4:payloadEnd] to cover the header (excluding magic) and payload
	payloadEnd := payloadStart + int(kLen) + int(vLen)
	h := crc32.NewIEEE()
	h.Write(buf[4:payloadEnd])
	checksum := h.Sum32()
	
	binary.LittleEndian.PutUint32(buf[totalSize-4:], checksum)

	// 6. Hand the fully serialized buffer off to the Group Commit engine
	req := LogRequest{
		Data: buf,
		Resp: make(chan error, 1),
	}
	
	w.reqCh <- req
	
	// Block until the background loop flushes this batch to disk
	return <-req.Resp
}

func (w *WAL) runGroupCommit() {
	for req := range w.reqCh {
		reqs := []LogRequest{req}
		
		// Collect more requests if available
		collect:
		for len(reqs) < 1024 { // max batch size
			select {
			case r := <-w.reqCh:
				reqs = append(reqs, r)
			default:
				break collect
			}
		}
		
		// Combine all buffers into one big write
		var combined []byte
		for _, r := range reqs {
			combined = append(combined, r.Data...)
		}
		
		_, err := w.file.Write(combined)
		if err == nil {
			err = w.file.Sync()
		}
		
		for _, r := range reqs {
			r.Resp <- err
		}
	}
}

