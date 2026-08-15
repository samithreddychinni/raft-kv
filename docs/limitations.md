# known limitations: technical debt

RaftKV is a project for understanding distributed systems. To keep the core logic clear, I've intentionally deferred certain production-level features.

---

## 1. connection setup

### connection pooling
Every heartbeat/append opens a fresh TCP connection.
Use persistent TCP connections when connection setup affects latency or CPU use.

---

## 2. feature gaps

### state WAL compaction
The key-value WAL compacts when a node restores a snapshot.
A running node keeps all applied commands in its WAL.
Compact the state WAL during normal operation when disk use becomes a problem.

### cluster membership changes
Peers are fixed at startup. §6 joint consensus for dynamic membership is not implemented.

### security
No authentication or TLS. Development/learning use only.

---

## resolved

### ~~linearizable reads~~

`GET /key/{key}` runs only on the leader. The leader confirms a quorum before it returns a value.
The server returns `503 Service Unavailable` when it cannot confirm leadership.

### ~~replication worker growth~~

Each leader starts one replication worker for each follower. Each worker permits one AppendEntries RPC at a time.
The workers stop when the node loses leadership.

### ~~Raft log compaction~~

Each node stores a snapshot after it applies 1,024 log entries.
The snapshot contains the key-value state and Raft log position.
The leader sends this snapshot to a follower that needs compacted entries. Snapshot transfer uses one RPC.

### ~~the persistence gap (§5.4)~~
~~`currentTerm` and `votedFor` stay in memory.~~

Fixed. Both fields are written to `<id>.raft.meta` via an atomic tmp+rename before any RPC reply is sent. The log is also fsync'd on every append. Recovery validates every record with CRC32 and auto-truncates corrupt tail bytes.

### ~~leader no-op entry on election (§5.4.2)~~

Fixed. When a node wins an election it immediately appends a no-op entry in its own term before initialising `nextIndex`/`matchIndex`.

**Why this matters** — the Figure-8 scenario in the paper: a leader from term T can replicate an entry E on a majority and then crash before advancing `commitIndex`. The next leader inherits E in its log but cannot commit it by counting replicas alone, because Raft's commit rule requires the counted entry to belong to the *current* term. Without a current-term entry to piggyback on, E sits uncommitted for the new leader's entire tenure.

The no-op gives the new leader an entry in its own term to replicate. Once it reaches a majority, `advanceCommitIndex` advances past it and E is committed as a side effect. No-op entries carry a nil command and are filtered out in `applyCommitted` so they never reach the KV store.
