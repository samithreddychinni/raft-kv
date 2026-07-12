// heartbeat: Leader loop that sends AppendEntries with actual log entries
package raft

import "time"

//leader loop wakes every peer worker on a heartbeat.
func (n *RaftNode) runLeaderLoop(leaderTerm uint64, stop <-chan struct{}) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	n.signalReplicationWorkers(leaderTerm)
	for {
		select {
		case <-ticker.C:
			n.signalReplicationWorkers(leaderTerm)
		case <-stop:
			return
		}
	}
}

func (n *RaftNode) signalReplicationWorkers(leaderTerm uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != Leader || n.currentTerm != leaderTerm {
		return
	}
	n.notifyReplicationWorkers()
}

//must be called with n.mu held.
func (n *RaftNode) notifyReplicationWorkers() {
	for _, trigger := range n.replicationTriggers {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}
}

//must be called with n.mu held.
func (n *RaftNode) stopReplicationWorkers() {
	if n.replicationStopCh != nil {
		close(n.replicationStopCh)
		n.replicationStopCh = nil
	}
	n.replicationTriggers = nil
}

//one worker per follower keeps one AppendEntries RPC in flight.
func (n *RaftNode) runReplicationWorker(p Peer, leaderTerm uint64, trigger <-chan struct{}, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-trigger:
			for n.sendAppendEntries(p, leaderTerm) {
			}
		}
	}
}

//returns true when a conflict needs another RPC right away.
func (n *RaftNode) sendAppendEntries(p Peer, leaderTerm uint64) bool {
	n.mu.Lock()
	if n.state != Leader || n.currentTerm != leaderTerm {
		n.mu.Unlock()
		return false
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
		return false
	}

	send := n.appendEntriesRPC
	if send == nil {
		send = sendRPC
	}
	var reply AppendEntriesReply
	if err := send(p.Addr, MsgAppendEntries, body, &reply); err != nil {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if reply.Term > n.currentTerm {
		n.becomeFollower(reply.Term)
		return false
	}

	if n.state != Leader || n.currentTerm != leaderTerm {
		return false
	}

	if reply.Success {
		match := args.PrevLogIndex + uint64(len(args.Entries))
		if match > n.matchIndex[p.ID] {
			n.matchIndex[p.ID] = match
			n.nextIndex[p.ID] = match + 1
			previousCommitIndex := n.commitIndex
			n.advanceCommitIndex()
			if n.commitIndex > previousCommitIndex {
				n.notifyReplicationWorkers()
			}
		}
		return n.nextIndex[p.ID] <= n.lastIndex()
	} else {
		previousNextIndex := n.nextIndex[p.ID]
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
		return n.nextIndex[p.ID] < previousNextIndex
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
