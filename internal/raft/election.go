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
	n.resetElectionTimer() // restart timer in case we don't win (Candidate to Candidate)

	term := n.currentTerm
	peers := n.peers
	log.Printf("[%s] → Candidate  term=%d", n.id, term)

	// broadcast RequestVote in parallel; results handled by goroutines
	args := RequestVoteArgs{
		Term:        term,
		CandidateID: n.id,
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

	// capture term for the goroutine; avoids holding the lock in the loop
	term := n.currentTerm
	peers := n.peers
	go n.runLeaderLoop(term, peers)
}

