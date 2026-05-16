# raftkv

a distributed key-value store built from scratch in Go.
no frameworks. no shortcuts. just the Raft paper, a terminal, and a lot of clusters that didn't survive the night.

---

## what it does

raftkv is a fault-tolerant key-value store that solves the hard part —
keeping data consistent when the world stops cooperating.

nodes crash. networks partition. leaders disappear mid-write.
raftkv handles all of it without flinching, without losing a single committed entry.


---

## what's inside

- **raft consensus** — full leader election, log replication, and conflict resolution across a live multi-node cluster. not a simplified version. the real algorithm.
- **write-ahead log** — every write is persisted to disk before it's acknowledged. checksummed, append-only, and recovers itself on restart without manual intervention.
- **http api** — `GET`, `SET`, `DELETE` over a clean REST interface. simple enough that the complexity is invisible.
- **cluster membership** — nodes find each other, elect a leader, and reach quorum. if a node falls behind, it catches up automatically.

---

## why it's built this way

this was built to survive — partial failures, split-brain scenarios, concurrent writes from multiple clients. every design decision has a reason, and that reason is usually *"what happens when this breaks at 3am?"*

the WAL doesn't just log writes. it validates them on recovery.
the Raft implementation doesn't just elect leaders. it handles the edge cases the paper warns you about in footnotes.

this is what it looks like when you build something to understand it completely,
not just to ship it.

---

## Technical Documentation

For deep-dives into the implementation and design decisions, see the following guides:

1.  [**Architecture Overview**](docs/architecture.md) — How the components fit together.
2.  [**Log Replication**](docs/raft_replication.md) — Consistency checks, conflict resolution, and commit logic.
3.  [**Leader Election**](docs/raft_election.md) — Quorum rules, randomized timeouts, and safety.
4.  [**Durability (WAL)**](docs/wal.md) — Wire format, checksums, and crash recovery.
5.  [**REST API**](docs/api.md) — Endpoints and leadership redirection.
6.  [**Known Limitations**](docs/limitations.md) — Technical debt and deferred features.

## Quick Start

### Build
```bash
go build -o raftkvd ./cmd/raftkvd
```

### Running a 3-Node Cluster
Open three terminals and run the following:

```bash
# Node 1
./raftkvd -id=node1 -http=:8081 -peer-addr=:9001 -raft-addr=:9101 \
  -peers=node2=:9002=:9102,node3=:9003=:9103

# Node 2
./raftkvd -id=node2 -http=:8082 -peer-addr=:9002 -raft-addr=:9102 \
  -peers=node1=:9001=:9101,node3=:9003=:9103

# Node 3
./raftkvd -id=node3 -http=:8083 -peer-addr=:9003 -raft-addr=:9103 \
  -peers=node1=:9001=:9101,node2=:9002=:9102
```

Once started, nodes will hold an election. You can send a write to any node, and it will redirect you to the leader via the `X-Raft-Leader` header if necessary.

```bash
curl -X POST -H "Content-Type: application/json" -d '{"value": "bar"}' http://localhost:8081/key/foo
curl -X GET http://localhost:8081/key/foo
curl -X DELETE http://localhost:8081/key/foo
```

## Tech Stack
- Go 1.21+
- Standard Library only (no external dependencies)

## License
MIT

---

built by [Samith Reddy](https://github.com/samithreddychinni) — 2026