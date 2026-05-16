# known limitations: technical debt

RaftKV is a project for understanding distributed systems. To keep the core logic clear, I've intentionally deferred certain production level optimizations.

---

## 1. the persistence gap (§5.4)
Raft requires `currentTerm` and `votedFor` to be written to disk before responding to any RPC. Right now, they stay in memory.
*   **The Risk**: If a node restarts, it loses its term/vote history. This could potentially allow a node to vote twice in the same term, violating safety.

## 2. resource leaks

### goroutine management
The leader spawns a new goroutine for every replication attempt. If a peer is down, these can pile up while waiting for a network timeout.
*   **The Fix**: Implementation of a worker pool or a dedicated replication manager per peer.

### connection pooling
Currently, every heartbeat or append opens a fresh TCP connection. 
*   **The Fix**: Use persistent TCP connections and reuse gob encoders/decoders.

## 3. feature gaps

### log compaction
The log grows indefinitely. Production systems use "snapshots" to discard old entries once they've been applied to the state machine. 

### linearizable reads
`GET` requests are served from the local node's memory. This is eventually consistent but not strictly linearizable. To be linearizable, the leader must confirm its authority with a majority before returning a read.

### security
There is no authentication or TLS. This is for development and learning only.
