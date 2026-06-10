// election: implements the Follower to Candidate to Leader transition path
// all decisions map directly to the state machine diagram transitions [docs/Raft_Node_State_machine_Diagram.png]
package raft

import (
	"log"
)

// startElection transitions this node from Follower to Candidate and
// broadcasts RequestVote RPCs to all peers concurrently
// Follower to Candidate (election timeout fires)
// must be called with n.mu held
func (n *RaftNode) startElection() {
	n.state = Candidate
	n.currentTerm++
	n.votedFor = n.id // vote for self (one vote per term)
	n.votes = 1
	n.persistMeta()        // term and vote changed must be durable before RPCs fly
	n.resetElectionTimer() // restart timer in case we don't win (Candidate to Candidate)

	term := n.currentTerm
	peers := n.peers
	log.Printf("[%s] → Candidate  term=%d", n.id, term)

	// broadcast RequestVote in parallel; results handled by goroutines
	args := RequestVoteArgs{
		Term:         term,
		CandidateID:  n.id,
		LastLogIndex: n.lastLogIndex, //let peers check log completeness
		LastLogTerm:  n.lastLogTerm,
	}
	body, err := encode(args)
	if err != nil {
		return
	}

	for _, p := range peers {
		go n.sendRequestVote(p, body, term)
	}

	// immediate quorum check: handles single-node cluster where no goroutines fire
	// cluster size includes self, so quorum = (len(peers)+1)/2 + 1
	clusterSize := len(peers) + 1
	quorum := clusterSize/2 + 1
	if n.votes >= quorum {
		n.becomeLeader()
	}
}

// sendRequestVote sends a single RequestVote RPC and processes the reply
func (n *RaftNode) sendRequestVote(p Peer, body []byte, electionTerm uint64) {
	var reply RequestVoteReply
	if err := sendRPC(p.Addr, MsgRequestVote, body, &reply); err != nil {
		// RPC failure: peer is unreachable, count it as no vote (no action needed)
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	// higher term rule: applies to both Candidate and any other state
	// Candidate to Follower (discovers higher term)
	if reply.Term > n.currentTerm {
		n.becomeFollower(reply.Term)
		return
	}

	// stale reply: election we care about no longer matches
	if n.state != Candidate || n.currentTerm != electionTerm {
		return
	}

	if reply.VoteGranted {
		n.votes++
		clusterSize := len(n.peers) + 1 // peers + self
		quorum := clusterSize/2 + 1
		if n.votes >= quorum {
			//Candidate to Leader (majority wins )
			n.becomeLeader()
		}
	}
}

// becomeLeader transitions to Leader and starts the heartbeat loop
// Candidate to Leader
// must be called with n.mu held
func (n *RaftNode) becomeLeader() {
	n.state = Leader
	n.electionTimer.Stop() // leaders don't time out into candidates
	log.Printf("[%s] → Leader  term=%d", n.id, n.currentTerm)

	n.nextIndex = make(map[string]uint64)
	n.matchIndex = make(map[string]uint64)

	// append a no-op entry in the new leader's term.
	//
	// a new leader may have log entries from a previous term that are
	// replicated on a majority but were never committed (the old leader
	// crashed before advancing commitIndex).  Raft's commit rule says a
	// leader can only commit an entry by counting replicas of an entry from
	// its *current* term.  Without a current-term entry to piggyback on,
	// those old entries could sit uncommitted for the entire tenure of this
	// leader +or get incorrectly committed by a later leader
	//
	// appending a no-op immediately gives us a current-term entry to
	// replicate.  once the no-op commits, advanceCommitIndex advances past
	// it and safely commits all preceding entries from prior terms.
	n.appendEntry(n.currentTerm, nil)

	nextIdx := n.lastIndex() + 1
	for _, p := range n.peers {
		n.nextIndex[p.ID] = nextIdx
		n.matchIndex[p.ID] = 0
	}

	// capture term for the goroutine; avoids holding the lock in the loop
	term := n.currentTerm
	peers := n.peers
	go n.runLeaderLoop(term, peers)
}
