// log: in-memory Raft log helpers + durable append/truncate
// all functions must be called with n.mu held
package raft

import "log"

// initLog creates the log with a zero-entry sentinel at index 0.
func (n *RaftNode) initLog() {
	n.raftLog = []LogEntry{{Index: 0, Term: 0}}
}

// lastIndex returns the index of the last entry in the log.
func (n *RaftNode) lastIndex() uint64 {
	return n.raftLog[len(n.raftLog)-1].Index
}

// lastTerm returns the term of the last entry in the log.
func (n *RaftNode) lastTerm() uint64 {
	return n.raftLog[len(n.raftLog)-1].Term
}

// entryAt returns the log entry at the given index, or false if out of range.
func (n *RaftNode) entryAt(index uint64) (LogEntry, bool) {
	if index >= uint64(len(n.raftLog)) {
		return LogEntry{}, false
	}
	return n.raftLog[index], true
}

// appendEntry appends a new entry to the in-memory log and persists it to disk.
// Returns the new entry's index.
// Must be called with n.mu held.
func (n *RaftNode) appendEntry(term uint64, cmd []byte) uint64 {
	idx := n.lastIndex() + 1
	entry := LogEntry{Index: idx, Term: term, Command: cmd}
	n.raftLog = append(n.raftLog, entry)
	n.lastLogIndex = idx
	n.lastLogTerm = term

	if n.persister != nil {
		if err := n.persister.AppendLogEntry(entry); err != nil {
			//a failed append means the log on disk is behind the in-memory log.
			//treat as fatal: returning success to the caller without durable
			//storage could cause data loss on crash.
			log.Fatalf("[%s] FATAL: persist log entry idx=%d: %v", n.id, idx, err)
		}
	}
	return idx
}

// truncateFrom removes all entries at and after idx from both the in-memory
// log and the on-disk log file.
// Must be called with n.mu held.
func (n *RaftNode) truncateFrom(idx uint64) {
	if idx >= uint64(len(n.raftLog)) {
		return
	}
	n.raftLog = n.raftLog[:idx]
	last := n.raftLog[len(n.raftLog)-1]
	n.lastLogIndex = last.Index
	n.lastLogTerm = last.Term

	if n.persister != nil {
		if err := n.persister.TruncateLogFrom(idx); err != nil {
			log.Fatalf("[%s] FATAL: truncate log from idx=%d: %v", n.id, idx, err)
		}
	}
}
