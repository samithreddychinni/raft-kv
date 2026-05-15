// heartbeat: Leader loop that sends AppendEntries with actual log entries
package raft

import (
	"time"
)

// runLeaderLoop fires replication RPCs every heartbeatInterval or when woken by Propose
func (n *RaftNode) runLeaderLoop(leaderTerm uint64, peers []Peer) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		n.mu.Lock()
		if n.state != Leader || n.currentTerm != leaderTerm {
			n.mu.Unlock()
			return
		}
		term := n.currentTerm
		n.mu.Unlock()

		for _, p := range peers {
			go n.sendAppendEntries(p, term)
		}

		select {
		case <-ticker.C:
		case <-n.wakeLoopCh:
		}
	}
}

// sendAppendEntries sends log entries to a peer and processes the reply
func (n *RaftNode) sendAppendEntries(p Peer, leaderTerm uint64) {
	n.mu.Lock()
	if n.state != Leader || n.currentTerm != leaderTerm {
		n.mu.Unlock()
		return
	}

	nextIdx := n.nextIndex[p.ID]
	prevLogIndex := nextIdx - 1
	prevLogTerm := uint64(0)
	if prevEntry, ok := n.entryAt(prevLogIndex); ok {
		prevLogTerm = prevEntry.Term
	}

	var entries []LogEntry
	if n.lastIndex() >= nextIdx {
		for i := nextIdx; i <= n.lastIndex(); i++ {
			if e, ok := n.entryAt(i); ok {
				entries = append(entries, e)
			}
		}
	}

	args := AppendEntriesArgs{
		Term:         leaderTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}
	n.mu.Unlock()

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

	if reply.Term > n.currentTerm {
		n.becomeFollower(reply.Term)
		return
	}

	if n.state != Leader || n.currentTerm != leaderTerm {
		return
	}

	if reply.Success {
		match := args.PrevLogIndex + uint64(len(args.Entries))
		if match > n.matchIndex[p.ID] {
			n.matchIndex[p.ID] = match
			n.nextIndex[p.ID] = match + 1
			n.advanceCommitIndex()
		}
	} else {
		//fast backward logic
		if reply.ConflictTerm > 0 {
			lastConflictIndex := uint64(0)
			for i := n.lastIndex(); i > 0; i-- {
				if e, ok := n.entryAt(i); ok && e.Term == reply.ConflictTerm {
					lastConflictIndex = i
					break
				}
			}
			if lastConflictIndex > 0 {
				n.nextIndex[p.ID] = lastConflictIndex + 1
			} else {
				n.nextIndex[p.ID] = reply.ConflictIndex
			}
		} else if reply.ConflictIndex > 0 {
			n.nextIndex[p.ID] = reply.ConflictIndex
		} else {
			if n.nextIndex[p.ID] > 1 {
				n.nextIndex[p.ID]--
			}
		}
	}
}

// advanceCommitIndex checks if a majority of followers have replicated new entries
// must be called with n.mu held
func (n *RaftNode) advanceCommitIndex() {
	newCommitIndex := n.commitIndex
	for nToTest := n.commitIndex + 1; nToTest <= n.lastIndex(); nToTest++ {
		e, ok := n.entryAt(nToTest)
		if !ok || e.Term != n.currentTerm {
			continue //check docs/raft-uncommitted-log-overwrite-seq.png
		}
		
		matchCount := 1 // self
		for _, p := range n.peers {
			if n.matchIndex[p.ID] >= nToTest {
				matchCount++
			}
		}
		
		clusterSize := len(n.peers) + 1
		quorum := clusterSize/2 + 1
		if matchCount >= quorum {
			newCommitIndex = nToTest
		}
	}

	if newCommitIndex > n.commitIndex {
		n.commitIndex = newCommitIndex
		n.applyCommitted()
	}
}
