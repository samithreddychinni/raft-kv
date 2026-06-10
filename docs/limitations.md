# known limitations: technical debt

RaftKV is a project for understanding distributed systems. To keep the core logic clear, I've intentionally deferred certain production-level features.

---

## 1. linearizable reads

`GET` requests are served from the local node's in-memory state. A follower can return stale data if it hasn't yet applied the latest committed entries.

To be strictly linearizable, the leader must confirm it is still the leader (via a read-index or lease-read quorum check) before returning any read result. This is deferred.

---

## 2. resource leaks

### goroutine management
The leader spawns a new goroutine for every replication attempt. If a peer is down, these pile up waiting for a network timeout.
*   **The Fix**: a worker pool or dedicated replication manager per peer.

### connection pooling
Every heartbeat/append opens a fresh TCP connection.
*   **The Fix**: persistent TCP connections with reused gob encoders/decoders.

---

## 3. feature gaps

### log compaction
The log grows indefinitely. Production systems use snapshots to discard applied entries.

### cluster membership changes
Peers are fixed at startup. §6 joint consensus for dynamic membership is not implemented.

### security
No authentication or TLS. Development/learning use only.

---

## resolved

### ~~the persistence gap (§5.4)~~
~~`currentTerm` and `votedFor` stay in memory.~~

Fixed. Both fields are written to `<id>.raft.meta` via an atomic tmp+rename before any RPC reply is sent. The log is also fsync'd on every append. Recovery validates every record with CRC32 and auto-truncates corrupt tail bytes.

### ~~leader no-op entry on election (§5.4.2)~~

Fixed. When a node wins an election it immediately appends a no-op entry in its own term before initialising `nextIndex`/`matchIndex`.

**Why this matters** — the Figure-8 scenario in the paper: a leader from term T can replicate an entry E on a majority and then crash before advancing `commitIndex`. The next leader inherits E in its log but cannot commit it by counting replicas alone, because Raft's commit rule requires the counted entry to belong to the *current* term. Without a current-term entry to piggyback on, E sits uncommitted for the new leader's entire tenure.

The no-op gives the new leader an entry in its own term to replicate. Once it reaches a majority, `advanceCommitIndex` advances past it and E is committed as a side effect. No-op entries carry a nil command and are filtered out in `applyCommitted` so they never reach the KV store.
