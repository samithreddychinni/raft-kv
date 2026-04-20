// rpc: on wire structs for RequestVote and AppendEntries RPCs
package raft

//MsgType constants extend the peer transport's MsgType enum
// values start at 10 to avoid collision with peer.MsGPing/MsgPong
const (
	MsgRequestVote  uint8 = 10
	MsgRequestVoteReply  uint8 = 11
	MsgAppendEntries  uint8 = 12
	MsgAppendEntriesReply uint8 = 13
)

// RequestVoteArgs is sent by a candidate to ask/solicit votes
type RequestVoteArgs struct {
	Term        uint64
	CandidateID string
	// LastLogIndex and LastLogTerm are used for log completeness check
	//for election only phase these are both 0
	LastLogIndex uint64
	LastLogTerm  uint64
}

// RequestVoteReply is the response from a peer to a RequestVote RPC
type RequestVoteReply struct {
	//lets the candidate detect a higher term and revert to follower
	Term        uint64
	VoteGranted bool
}

// AppendEntriesArgs is sent by the Leader as a heartbeat or with real log entries
type AppendEntriesArgs struct {
	Term         uint64
	LeaderID     string
	PrevLogIndex uint64 // index of entry immediately before new ones
	PrevLogTerm  uint64 // term of that entry
	Entries      []LogEntry // empty slice = heartbeat
	LeaderCommit uint64 // leader's current commitIndex
}

//LogEntry is a single command in the replicated log
type LogEntry struct {
	Index   uint64
	Term    uint64
	Command []byte // opcode + key + value, encoded by the leader
}

//AppendEntriesReply lets the leader detect a higher term and step down
type AppendEntriesReply struct {
	Term    uint64
	Success bool
	ConflictIndex uint64 // first index of the conflicting term (fast catch-up hint)
	ConflictTerm  uint64 // term at that index; 0 if follower log is too short
}