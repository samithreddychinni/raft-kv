// heartbeat: Leader loop that sends AppendEntries (empty) to maintain authority
// Leader sends heartbeats, steps down to Follower on higher term
package raft

import (
	"log"
	"time"
)

// runLeaderLoop fires heartbeats every heartbeatInterval
// it exits as soon as this node is no longer Leader
// Leader to Follower (discovers higher term in any heartbeat reply)
func (n *RaftNode) runLeaderLoop(leaderTerm uint64, peers []Peer) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		n.mu.Lock()
		// stop if we have stepped down since the loop started
		if n.state != Leader || n.currentTerm != leaderTerm {
			n.mu.Unlock()
			return
		}
		term := n.currentTerm
		n.mu.Unlock()

		log.Printf("[%s] sending heartbeat  term=%d", n.id, term)
		for _, p := range peers {
			go n.sendHeartbeat(p, term)
		}
	}
}

// sendHeartbeat sends a single empty AppendEntries RPC and processes the reply
func (n *RaftNode) sendHeartbeat(p Peer, leaderTerm uint64) {
	args := AppendEntriesArgs{
		Term:     leaderTerm,
		LeaderID: n.id,
	}
	body, err := encode(args)
	if err != nil {
		return
	}

	var reply AppendEntriesReply
	if err := sendRPC(p.Addr, MsgAppendEntries, body, &reply); err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	// higher term rule: if any reply carries a higher term, step down
	// Leader to Follower (discovers higher term)[docs/Raft_Node_State_machine_Diagram.png]
	if reply.Term > n.currentTerm {
		n.becomeFollower(reply.Term)
	}
}
