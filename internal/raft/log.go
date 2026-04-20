// log: in-memory Raft log helpers
// for future me :- all functions must be called with n.mu held
package raft

//initLog creates the log with a zero-entry sentinel at index 0
func (n *RaftNode) initLog() {
	n.raftLog = []LogEntry{{Index: 0, Term: 0}}
}

//lastIndex returns the index of the last entry in the log
func (n *RaftNode) lastIndex() uint64 {
	return n.raftLog[len(n.raftLog)-1].Index
}

//lastTerm returns the term of the last entry in the log
func (n *RaftNode) lastTerm() uint64 {
	return n.raftLog[len(n.raftLog)-1].Term
}

//entryAt returns the log entry at the given index, or false if out of range
func (n *RaftNode) entryAt(index uint64) (LogEntry, bool) {
	if index >= uint64(len(n.raftLog)) {
		return LogEntry{}, false
	}
	return n.raftLog[index], true
}

//appendEntry appends a new entry to the log and returns its index
func (n *RaftNode) appendEntry(term uint64, cmd []byte) uint64 {
	idx := n.lastIndex() + 1
	n.raftLog = append(n.raftLog, LogEntry{Index: idx, Term: term, Command: cmd})
	n.lastLogIndex = idx
	n.lastLogTerm = term
	return idx
}

//truncateFrom removes all entries at and after idx
func (n *RaftNode) truncateFrom(idx uint64) {
	if idx < uint64(len(n.raftLog)) {
		n.raftLog = n.raftLog[:idx]
		last := n.raftLog[len(n.raftLog)-1]
		n.lastLogIndex = last.Index
		n.lastLogTerm = last.Term
	}
}
