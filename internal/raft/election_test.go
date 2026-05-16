// election_test: unit tests for the Raft state machine voting logic.
// All tests are purely in-memory (no network) using direct method calls.
package raft

import (
	"testing"
	"time"
)

// makeNode builds a RaftNode with a stopped timer so timeouts don't fire during tests.
func makeNode(id string, peers []Peer) *RaftNode {
	n := &RaftNode{
		id:            id,
		addr:          ":0",
		peers:         peers,
		state:         Follower,
		applyCh:       make(chan LogEntry, 256),
		electionTimer: time.AfterFunc(24*time.Hour, func() {}), // effectively stopped
	}
	n.initLog()
	return n
}

// --- Voting rule tests ---

func TestGrantVote_FirstRequest(t *testing.T) {
	n := makeNode("n1", nil)

	reply := n.HandleRequestVote(RequestVoteArgs{Term: 1, CandidateID: "n2"})

	if !reply.VoteGranted {
		t.Fatal("expected vote granted on first request in term")
	}
	if n.votedFor != "n2" {
		t.Fatalf("votedFor = %q; want n2", n.votedFor)
	}
}

func TestDenyVote_AlreadyVoted(t *testing.T) {
	n := makeNode("n1", nil)
	n.currentTerm = 1
	n.votedFor = "n3" // already voted for someone else this term

	reply := n.HandleRequestVote(RequestVoteArgs{Term: 1, CandidateID: "n2"})

	if reply.VoteGranted {
		t.Fatal("node must not grant a second vote in the same term")
	}
}

func TestGrantVote_SameCandidate(t *testing.T) {
	// Idempotent: voting for same candidate again is permitted.
	n := makeNode("n1", nil)
	n.currentTerm = 1
	n.votedFor = "n2"

	reply := n.HandleRequestVote(RequestVoteArgs{Term: 1, CandidateID: "n2"})

	if !reply.VoteGranted {
		t.Fatal("expected vote granted when re-voting for the same candidate")
	}
}

func TestDenyVote_StaleTerm(t *testing.T) {
	n := makeNode("n1", nil)
	n.currentTerm = 5

	reply := n.HandleRequestVote(RequestVoteArgs{Term: 2, CandidateID: "n2"})

	if reply.VoteGranted {
		t.Fatal("must deny vote from a candidate with stale term")
	}
	if reply.Term != 5 {
		t.Fatalf("should return own term=5, got %d", reply.Term)
	}
}

// --- Higher term / state machine transition tests ---

func TestHigherTerm_RequestVoteRestoreFollower(t *testing.T) {
	// A Candidate that receives a vote with a higher term must revert to Follower.
	n := makeNode("n1", nil)
	n.state = Candidate
	n.currentTerm = 3

	// incoming RequestVote carries term=5 (higher)
	n.HandleRequestVote(RequestVoteArgs{Term: 5, CandidateID: "n2"})

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != Follower {
		t.Fatalf("state = %s; want Follower after higher term in RequestVote", n.state)
	}
	if n.currentTerm != 5 {
		t.Fatalf("currentTerm = %d; want 5", n.currentTerm)
	}
}

func TestHigherTerm_AppendEntriesRestoreFollower(t *testing.T) {
	// Diagram: Leader → Follower (discovers higher term in AppendEntries).
	n := makeNode("n1", nil)
	n.state = Leader
	n.currentTerm = 3

	n.HandleAppendEntries(AppendEntriesArgs{Term: 4, LeaderID: "n2"})

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != Follower {
		t.Fatalf("state = %s; want Follower after higher term in AppendEntries", n.state)
	}
	if n.currentTerm != 4 {
		t.Fatalf("currentTerm = %d; want 4", n.currentTerm)
	}
}

func TestAppendEntries_StaleTerm_Rejected(t *testing.T) {
	n := makeNode("n1", nil)
	n.currentTerm = 5

	reply := n.HandleAppendEntries(AppendEntriesArgs{Term: 2, LeaderID: "n2"})

	if reply.Success {
		t.Fatal("must reject AppendEntries from a stale leader")
	}
}

func TestAppendEntries_ValidLeader_CandidateReverts(t *testing.T) {
	// Diagram: Candidate → Follower (discovers leader with term >= currentTerm).
	n := makeNode("n1", nil)
	n.state = Candidate
	n.currentTerm = 2

	reply := n.HandleAppendEntries(AppendEntriesArgs{Term: 2, LeaderID: "n2"})

	if !reply.Success {
		t.Fatal("expected Success=true from valid leader")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != Follower {
		t.Fatalf("state = %s; want Follower after valid AppendEntries", n.state)
	}
}

// --- Election triggering tests ---

func TestStartElection_TermIncrements(t *testing.T) {
	// Two-node cluster: quorum = 2, so self-vote alone won't promote to Leader.
	// This lets us observe the Candidate state before any peer replies.
	n := makeNode("n1", []Peer{{ID: "n2", Addr: ":0"}})
	n.currentTerm = 2

	n.mu.Lock()
	n.startElection()
	term := n.currentTerm
	state := n.state
	voted := n.votedFor
	n.mu.Unlock()

	if state != Candidate {
		t.Fatalf("state = %s; want Candidate", state)
	}
	if term != 3 {
		t.Fatalf("currentTerm = %d; want 3 after election start", term)
	}
	if voted != "n1" {
		t.Fatalf("votedFor = %q; want n1 (self)", voted)
	}
}

func TestStartElection_VotesSelf(t *testing.T) {
	n := makeNode("n1", nil)

	n.mu.Lock()
	n.startElection()
	votes := n.votes
	n.mu.Unlock()

	if votes != 1 {
		t.Fatalf("votes = %d; want 1 (self-vote)", votes)
	}
}

func TestMajority_SingleNodeBecomesLeader(t *testing.T) {
	// A single-node cluster has no peers; it should immediately win.
	n := makeNode("n1", nil /* no peers */)

	n.mu.Lock()
	n.startElection()
	// With 0 peers, quorum = 0/2+1 = 1, and n.votes = 1 → leader immediately.
	state := n.state
	n.mu.Unlock()

	if state != Leader {
		t.Fatalf("state = %s; want Leader in single-node cluster", state)
	}
}
