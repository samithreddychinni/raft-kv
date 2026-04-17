// rpc: on wire structs for RequestVote and AppendEntries RPCs
package raft

//MsgType constants extend the peer transport's MsgType enum
// values start at 10 to avoid collision with peer.MsGPing/MsgPong
const (
	MsgRequestVote      uint8 = 10
	MsgRequestVoteReply uint8 = 11
	MsgAppendEntries    uint8 = 12
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

// AppendEntriesArgs is sent by the Leader as a heartbeat [entries is empty]
//or with log entries for replication here we only use it for heartbeats
type AppendEntriesArgs struct {
	Term     uint64
	LeaderID string
	//entries is empty for heartbeat RPCs
	Entries []LogEntry
}

//LogEntry is a placeholder; log replication is a future plan
type LogEntry struct {
	Term    uint64
	Command []byte
}

//AppendEntriesReply lets the leader detect a higher term and step down
type AppendEntriesReply struct {
	Term    uint64
	Success bool
}
