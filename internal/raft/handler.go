// handler: inbound RPC handlers the "receiver" side of RequestVote and AppendEntries
// together these implement the vote granting and follower reset rules from the diagram [docs/Raft_Node_State_machine_Diagram.png]
package raft

import "log"

// HandleRequestVote processes an incoming RequestVote RPC from a Candidate
func (n *RaftNode) HandleRequestVote(args RequestVoteArgs) RequestVoteReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := RequestVoteReply{Term: n.currentTerm}

	// stale candidate.
	if args.Term < n.currentTerm {
		return reply
	}

	// Higher term rule: any higher term forces us to Follower
	// Candidate/Leader to Follower (discovers higher term in RequestVote)
	if args.Term > n.currentTerm {
		n.becomeFollower(args.Term)
	}

	// log completeness check: refuse to vote for a candidate whose
	// log is less up-to-date than ours. Candidate log is "at least as up-to-date" if:
	//   a) its LastLogTerm is higher, OR
	//   b) same LastLogTerm and its LastLogIndex >= ours.
	candidateLogOK := args.LastLogTerm > n.lastLogTerm ||
		(args.LastLogTerm == n.lastLogTerm && args.LastLogIndex >= n.lastLogIndex)

	// one vote per term: grant only if we have not voted yet (or voted for same candidate)
	// AND the candidate's log is at least as up-to-date as ours.
	if (n.votedFor == "" || n.votedFor == args.CandidateID) && candidateLogOK {
		n.votedFor = args.CandidateID
		n.resetElectionTimer() // a real leader will appear soon; reset timer
		reply.VoteGranted = true
		reply.Term = n.currentTerm
		log.Printf("[%s] granted vote → %s  term=%d", n.id, args.CandidateID, n.currentTerm)
	}

	return reply
}

// HandleAppendEntries processes an incoming AppendEntries RPC from a Leader
//
// decision map:
//
//	1)args.Term < currentTerm  to stale leader, reject
//	2)args.Term >= currentTerm to valid leader; revert to Follower if needed (Higher Term / Discovers Leader rules)
//	                              reset election timer
func (n *RaftNode) HandleAppendEntries(args AppendEntriesArgs) AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := AppendEntriesReply{Term: n.currentTerm}

	// stale leader
	if args.Term < n.currentTerm {
		return reply
	}

	// Higher term or valid leader discovered — two diagram transitions collapse here:
	//   Candidate to Follower (discovers valid leader with term >= currentTerm)
	//   Leader to Follower (discovers higher term)
	if args.Term > n.currentTerm || n.state != Follower {
		n.becomeFollower(args.Term)
	} else {
		// same term, already Follower: then just reset the timer
		n.resetElectionTimer()
	}

	log.Printf("[%s] heartbeat ← %s  term=%d", n.id, args.LeaderID, args.Term)
	reply.Term = n.currentTerm
	reply.Success = true
	return reply
}
