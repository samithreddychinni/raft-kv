# RaftKV: In-Memory Key-Value Store (v0.2.0)

## What is this?

RaftKV is a personal project for exploring depths of distributed systems and Golang. The current version is a single-node in-memory key-value store with an HTTP API and a Write-Ahead Log for durability. This is the foundation - distributed consensus (Raft)[https://raft.github.io/raft.pdf] and clustering are planned but not yet implemented.

## Current Features (v0.2.0)

- In-memory key-value storage with mutex protection.
- **Write-Ahead Log (WAL)** — data survives server restarts.
- HTTP REST API with endpoints:
  - `GET /` - retrieve all key-value pairs
  - `GET /key/{key}` - get value for a key
  - `POST /key/{key}` - set key-value pair (JSON body: `{"value": "..."}`)
  - `DELETE /key/{key}` - delete a key
- Simple server implementation using Go's standard library.

## Running the Project

```bash
# Build
go build -o raftkvd ./cmd/raftkvd

# Run
./raftkvd
# Server starts on http://localhost:8080
# WAL file created at raftkvd.wal in the same directory

# Or run directly
go run ./cmd/raftkvd
```

## Testing the API

```bash
# Set a key
curl -X POST http://localhost:8080/key/hello \
  -H "Content-Type: application/json" \
  -d '{"value": "world"}'

# Get a key
curl http://localhost:8080/key/hello
# Returns: "world"

# Get all keys
curl http://localhost:8080/

# Delete a key
curl -X DELETE http://localhost:8080/key/hello
```

## Write-Ahead Log (WAL)

### What exists

The WAL lives in `internal/wal/` and is split across three files.

`wal.go` defines the wire format and shared constants. `wal_writer.go` owns the write path — every `AppendSet` and `AppendDelete` call builds a 16-byte header and an fsync'd entry on disk before returning. `wal_reader.go` owns the read path — `ReadEntry` decodes one entry at a time, validates the magic number and CRC32 checksum, and surfaces typed errors for corruption.

The store in `internal/store/store.go` owns a WAL handle. On startup it replays the entire log into the in-memory map before accepting traffic. On every write it logs to disk first, then updates the map.

### Wire format

```
[magic: 4B @ 0][key_len: 4B @ 4][val_len: 4B @ 8][opcode: 1B @ 12][version: 1B @ 13][reserved: 2B @ 14]
...key bytes...value bytes...[CRC32: 4B]
```

All integers are little-endian. The four-byte fields are placed first so every `uint32` lands on a naturally aligned offset such tht no split-word fetches on x86. The checksum covers header bytes `[4:16]` plus the key and value, so any bit flip in the lengths, opcode, or data is detected.

### Why these decisions

**Magic number `0xDEADBEEF`** — a recognisable sentinel at the start of every entry. If the process crashes mid-write the next startup finds a header where the magic does not match and stops reading at that point instead of silently applying garbage.

**CRC32 over header + body** — protects against torn writes where the magic survived but the payload was partially flushed. Including the header bytes in the checksum means a corrupted key_len or val_len is also caught before a bad allocation happens.

**16-byte aligned header** — the extra three bytes beyond the minimum 13 carry a `version` field and two reserved bytes zeroed on write. The version field lets us change the format in a future release without crashing on old log files. The reserved bytes are available for flags like compression or encryption without a format bump.

**fsync on every append** — `file.Sync()` is called before the write returns. This guarantees strict durability (no acknowledged writes are lost in a crash).

**WAL-first write ordering** — the store writes to the log before touching the in-memory map. A crash between the two leaves the entry in the log. On replay that entry is re-applied, which is safe because SET is idempotent and a redundant DELETE is harmless.

**Truncation on corruption** — if `ReadEntry` returns any error other than `io.EOF` during replay, the store truncates the file at the last known clean offset. This discards the torn entry so the next startup does not fail on the same bad bytes.

### Future Optimizations

- **True Synchronous Group Commit:** Currently, every write triggers an immediate, dedicated fsync. This provides absolute safety but bottlenecks throughput to the physical IOPS limit of the disk. A known production optimization is buffering multiple concurrent writes into a batch, blocking all client responses, and executing a single fsync for the entire group. This drastically increases throughput by amortizing the I/O cost while maintaining strict durability (unlike asynchronous batching, which risks silent data loss on crash).
- **Log Compaction and Rotation:** The WAL is append-only and grows forever. Background snapshotting and log file rotation are planned for v0.3.x.
- **Version Enforcement:** The `reserved` header bytes and `version` field are built into the wire format but not yet enforced or acted upon during replay.

## Known Limitations

- Single-node only, no replication or fault tolerance.
- No authentication or security.

## Project Goals

This is a learning project with planned phases:
1. **v0.1.x** (done) - Single-node in-memory store
2. **v0.2.x** (current) - Write-Ahead Log for single-node durability
3. v0.3.x - Log compaction, snapshots, WAL rotation
4. v0.4.x - Raft consensus for single-node mode (leader only)
5. v0.5.x - Multi-node cluster support with leader election and log replication
6. Chaos testing infrastructure to simulate failures and validate fault tolerance

## Tech Stack

- Go 1.21+
- Standard library (net/http, sync, encoding/json, hash/crc32)

## Why ?

I'm using this project to deeply understand distributed systems by building one from the ground up - rather than "vibe coding" solutions. The follow-up phase will include a Chaos Monkey-style failure simulator to test the system's resilience.
