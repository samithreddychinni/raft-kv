// linearizable: ReadIndex linearizable reads [https://raft.github.io/raft.pdf §6.4]
package raft

import (
	"fmt"
	"time"
)

var ErrReadTimeout = fmt.Errorf("raft: read index timed out")

const (
	readIndexTimeout = 2 * time.Second
	readPollInterval = 1 * time.Millisecond
)

// ReadIndex returns an index safe to read at; on return lastApplied >= it.
func (n *RaftNode) ReadIndex() (uint64, error) {
	n.mu.Lock()
	if n.state != Leader {
		n.mu.Unlock()
		return 0, ErrNotLeader
	}
	term := n.currentTerm
	n.mu.Unlock()

	deadline := time.Now().Add(readIndexTimeout)

	// wait for a current-term entry to commit (the election no-op guarantees
	// one) so commitIndex reflects the true latest committed state
	for {
		n.mu.Lock()
		if n.state != Leader || n.currentTerm != term {
			n.mu.Unlock()
			return 0, ErrNotLeader
		}
		e, ok := n.entryAt(n.commitIndex)
		ready := ok && e.Term == term
		n.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			return 0, ErrReadTimeout
		}
		time.Sleep(readPollInterval)
	}

	n.mu.Lock()
	readIndex := n.commitIndex
	n.mu.Unlock()

	if !n.confirmLeadership(term) {
		return 0, ErrNotLeader
	}

	for {
		n.mu.Lock()
		applied := n.lastApplied
		st := n.state
		ct := n.currentTerm
		n.mu.Unlock()
		if st != Leader || ct != term {
			return 0, ErrNotLeader
		}
		if applied >= readIndex {
			return readIndex, nil
		}
		if time.Now().After(deadline) {
			return 0, ErrReadTimeout
		}
		time.Sleep(readPollInterval)
	}
}

// confirmLeadership reports whether a majority still recognises us as leader
// for term. a lagging follower still counts; only a higher term deposes us.
func (n *RaftNode) confirmLeadership(term uint64) bool {
	n.mu.Lock()
	if n.state != Leader || n.currentTerm != term {
		n.mu.Unlock()
		return false
	}
	peers := n.peers
	n.mu.Unlock()

	if len(peers) == 0 {
		return true
	}

	acks := make(chan bool, len(peers))
	for _, p := range peers {
		go func(p Peer) {
			acks <- n.sendConfirmHeartbeat(p, term)
		}(p)
	}

	granted := 1
	quorum := (len(peers)+1)/2 + 1
	timeout := time.After(dialTimeout)

	for i := 0; i < len(peers); i++ {
		select {
		case ok := <-acks:
			if ok {
				granted++
				if granted >= quorum {
					return true
				}
			}
		case <-timeout:
			return granted >= quorum
		}
	}
	return granted >= quorum
}

func (n *RaftNode) sendConfirmHeartbeat(p Peer, term uint64) bool {
	n.mu.Lock()
	if n.state != Leader || n.currentTerm != term {
		n.mu.Unlock()
		return false
	}
	nextIdx := n.nextIndex[p.ID]
	prevLogIndex := nextIdx - 1
	prevLogTerm := uint64(0)
	if prevEntry, ok := n.entryAt(prevLogIndex); ok {
		prevLogTerm = prevEntry.Term
	}
	args := AppendEntriesArgs{
		Term:         term,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      nil,
		LeaderCommit: n.commitIndex,
	}
	n.mu.Unlock()

	body, err := encode(args)
	if err != nil {
		return false
	}

	var reply AppendEntriesReply
	if err := sendRPC(p.Addr, MsgAppendEntries, body, &reply); err != nil {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if reply.Term > n.currentTerm {
		n.becomeFollower(reply.Term)
		return false
	}
	return n.state == Leader && n.currentTerm == term
}
