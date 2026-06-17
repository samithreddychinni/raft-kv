// noop_test: leader no-op fix for the stale-entry case [§5.4.2]
package raft

import (
	"testing"
	"time"
)

func TestLeaderAppendsNoopOnElection(t *testing.T) {
	n := makeNode("n1", nil)
	n.currentTerm = 2

	n.mu.Lock()
	n.becomeLeader()
	logLen := len(n.raftLog)
	entry := n.raftLog[1]
	n.mu.Unlock()

	if logLen != 2 {
		t.Fatalf("log length = %d; want 2 (sentinel + no-op)", logLen)
	}
	if entry.Term != 2 {
		t.Fatalf("no-op term = %d; want 2", entry.Term)
	}
	if len(entry.Command) != 0 {
		t.Fatalf("no-op command = %v; want nil", entry.Command)
	}
}

func TestNoopCommitUnblocksStaleEntry(t *testing.T) {
	n := makeNode("n1", nil)
	n.mu.Lock()
	n.currentTerm = 1
	n.appendEntry(1, []byte("set x=1"))
	n.currentTerm = 2
	n.becomeLeader()
	n.advanceCommitIndex()

	committed := n.commitIndex
	n.mu.Unlock()

	if committed < 2 {
		t.Fatalf("commitIndex = %d; want >= 2 — stale entry not unblocked", committed)
	}
}

func TestNoopNotDeliveredToApplyCh(t *testing.T) {
	n := makeNode("n1", nil)
	n.mu.Lock()
	n.currentTerm = 1
	n.becomeLeader()
	n.advanceCommitIndex()
	n.mu.Unlock()

	select {
	case entry := <-n.applyCh:
		t.Fatalf("no-op leaked into applyCh: index=%d cmd=%v", entry.Index, entry.Command)
	case <-time.After(20 * time.Millisecond):
	}
}
