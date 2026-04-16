# RaftKV: In-Memory Key-Value Store (v0.1.0)

## What is this?

RaftKV is a personal project for exploring depths of distributed systems and Golang. The current version is a simple single-node in-memory key-value store with an HTTP API. This is the foundation - distributed consensus (Raft)[https://raft.github.io/raft.pdf] and clustering are planned but not yet implemented.

## Current Features (v0.1.0)

- In-memory key-value storage with mutex protection.
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

## Known Limitations

- Data is in-memory only - lost on restart.
- Single-node only, no replication or fault tolerance.
- No persistence to disk.
- No authentication or security.
- Basic error handling only.

## Project Goals

This is a learning project with planned phases:
1. **v0.1.x** (current) - Single-node in-memory store
2. v0.2.x - Add Raft consensus for single-node mode (leader only)
3. v0.3.x - Multi-node cluster support with leader election and log replication
4. v0.4.x - Persistence, snapshots, and cluster membership changes
5. Chaos testing infrastructure to simulate failures and validate fault tolerance

## Tech Stack

- Go 1.21+
- Standard library (net/http, sync, encoding/json)

## Motivation

I'm using this project to deeply understand distributed systems by building one from the ground up - rather than "vibe coding" solutions. The follow-up phase will include a Chaos Monkey-style failure simulator to test the system's resilience.
