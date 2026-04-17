// state:-raftstate enum
package raft

// RaftState is the current role of this node in the cluster
type RaftState int

const (
	Follower  RaftState = iota //passive:-reset timer on heartbeat
	Candidate                  //election in progress
	Leader                     //authority:-sends heartbeats
)

func (s RaftState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}
