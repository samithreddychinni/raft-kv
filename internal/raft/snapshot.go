package raft

import (
	"fmt"
	"log"
	"time"
)

const snapshotApplyTimeout = 2 * time.Second

// SnapshotIndex returns the highest index included in the local snapshot.
func (n *RaftNode) SnapshotIndex() uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.snapshot.LastIncludedIndex
}

// Compact stores data for index and discards all Raft log entries through it.
// The caller must take data after its state machine applies index.
func (n *RaftNode) Compact(index uint64, data []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if index <= n.snapshot.LastIncludedIndex {
		return nil
	}
	if index > n.stateApplied.Load() {
		return fmt.Errorf("raft: snapshot index %d is not applied", index)
	}
	entry, ok := n.entryAt(index)
	if !ok {
		return fmt.Errorf("raft: snapshot index %d is not in the log", index)
	}

	snapshot := Snapshot{
		LastIncludedIndex: index,
		LastIncludedTerm:  entry.Term,
		Data:              append([]byte(nil), data...),
	}
	tail := n.logAfter(index)
	if n.persister != nil {
		if err := n.persister.SaveSnapshot(snapshot); err != nil {
			log.Fatalf("[%s] FATAL: persist snapshot: %v", n.id, err)
		}
		if err := n.persister.ReplaceLog(tail); err != nil {
			log.Fatalf("[%s] FATAL: compact log: %v", n.id, err)
		}
	}
	n.installSnapshot(snapshot, tail)
	return nil
}

// HandleInstallSnapshot replaces this node state through the snapshot index.
func (n *RaftNode) HandleInstallSnapshot(args InstallSnapshotArgs) InstallSnapshotReply {
	n.mu.Lock()
	reply := InstallSnapshotReply{Term: n.currentTerm}
	if args.Term < n.currentTerm {
		n.mu.Unlock()
		return reply
	}
	if args.Term > n.currentTerm || n.state != Follower {
		n.becomeFollower(args.Term)
	} else {
		n.resetElectionTimer()
	}
	reply.Term = n.currentTerm
	if n.installingSnapshot {
		n.mu.Unlock()
		return reply
	}
	if args.LastIncludedIndex <= n.commitIndex {
		reply.Success = true
		n.mu.Unlock()
		return reply
	}

	// Let the state machine finish every command already sent to it. This
	// prevents an older queued command from overwriting the snapshot state.
	n.installingSnapshot = true
	dispatched := n.lastApplied
	n.mu.Unlock()
	deadline := time.Now().Add(snapshotApplyTimeout)
	for n.stateApplied.Load() < dispatched {
		if time.Now().After(deadline) {
			n.mu.Lock()
			n.installingSnapshot = false
			reply.Term = n.currentTerm
			n.mu.Unlock()
			return reply
		}
		time.Sleep(time.Millisecond)
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	defer func() { n.installingSnapshot = false }()
	if n.currentTerm != args.Term {
		reply.Term = n.currentTerm
		return reply
	}

	snapshot := Snapshot{
		LastIncludedIndex: args.LastIncludedIndex,
		LastIncludedTerm:  args.LastIncludedTerm,
		Data:              append([]byte(nil), args.Data...),
	}
	tail := []LogEntry(nil)
	if entry, ok := n.entryAt(args.LastIncludedIndex); ok && entry.Term == args.LastIncludedTerm {
		tail = n.logAfter(args.LastIncludedIndex)
	}
	if n.persister != nil {
		if err := n.persister.SaveSnapshot(snapshot); err != nil {
			log.Fatalf("[%s] FATAL: persist received snapshot: %v", n.id, err)
		}
	}
	if n.restoreSnapshot != nil {
		if err := n.restoreSnapshot(snapshot.Data); err != nil {
			log.Fatalf("[%s] FATAL: restore received snapshot: %v", n.id, err)
		}
	}
	if n.persister != nil {
		if err := n.persister.ReplaceLog(tail); err != nil {
			log.Fatalf("[%s] FATAL: compact received snapshot: %v", n.id, err)
		}
	}
	n.installSnapshot(snapshot, tail)
	reply.Success = true
	return reply
}

// installSnapshot updates the in-memory log after snapshot data is durable.
// Must be called with n.mu held.
func (n *RaftNode) installSnapshot(snapshot Snapshot, tail []LogEntry) {
	n.snapshot = snapshot
	n.raftLog = append([]LogEntry{{
		Index: snapshot.LastIncludedIndex,
		Term:  snapshot.LastIncludedTerm,
	}}, tail...)
	n.lastLogIndex = n.lastIndex()
	n.lastLogTerm = n.lastTerm()
	if n.commitIndex < snapshot.LastIncludedIndex {
		n.commitIndex = snapshot.LastIncludedIndex
	}
	if n.lastApplied < snapshot.LastIncludedIndex {
		n.lastApplied = snapshot.LastIncludedIndex
	}
	n.Applied(snapshot.LastIncludedIndex)
}

// logAfter returns a copy of entries strictly after index.
// Must be called with n.mu held.
func (n *RaftNode) logAfter(index uint64) []LogEntry {
	start := index - n.raftLog[0].Index + 1
	if start >= uint64(len(n.raftLog)) {
		return nil
	}
	return append([]LogEntry(nil), n.raftLog[start:]...)
}
