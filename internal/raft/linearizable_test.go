package raft

import (
	"testing"
	"time"
)

func TestReadIndex_RejectsNonLeader(t *testing.T) {
	n := makeNode("n1", nil)

	_, err := n.ReadIndex()
	if err != ErrNotLeader {
		t.Fatalf("err = %v; want ErrNotLeader", err)
	}
}

func TestReadIndex_LeaderReturnsCommitIndex(t *testing.T) {
	n := makeNode("n1", nil)

	n.mu.Lock()
	n.currentTerm = 1
	n.becomeLeader()
	n.advanceCommitIndex()
	n.applyCommitted()
	n.mu.Unlock()

	idx, err := n.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex error: %v", err)
	}
	if idx < 1 {
		t.Fatalf("readIndex = %d; want >= 1", idx)
	}

	n.mu.Lock()
	applied := n.lastApplied
	n.mu.Unlock()
	if applied < idx {
		t.Fatalf("lastApplied = %d; must be >= readIndex %d on return", applied, idx)
	}
}

func TestReadIndex_TimesOutWhenCurrentTermEntryNeverCommits(t *testing.T) {
	n := makeNode("n1", []Peer{{ID: "n2", Addr: ":0"}})

	n.mu.Lock()
	n.currentTerm = 1
	n.becomeLeader()
	n.mu.Unlock()

	start := time.Now()
	_, err := n.ReadIndex()
	elapsed := time.Since(start)

	if err != ErrReadTimeout {
		t.Fatalf("err = %v; want ErrReadTimeout", err)
	}
	if elapsed < readIndexTimeout {
		t.Fatalf("returned after %v; want >= %v", elapsed, readIndexTimeout)
	}
}

func TestConfirmLeadership_SingleNode(t *testing.T) {
	n := makeNode("n1", nil)
	n.mu.Lock()
	n.currentTerm = 1
	n.state = Leader
	n.mu.Unlock()

	if !n.confirmLeadership(1) {
		t.Fatal("single-node leader must confirm its own leadership")
	}
}

func TestConfirmLeadership_StaleTermFails(t *testing.T) {
	n := makeNode("n1", nil)
	n.mu.Lock()
	n.currentTerm = 5
	n.state = Leader
	n.mu.Unlock()

	if n.confirmLeadership(3) {
		t.Fatal("must fail for a stale term")
	}
}
