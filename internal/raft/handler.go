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
func (n *RaftNode) HandleAppendEntries(args AppendEntriesArgs) AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := AppendEntriesReply{Term: n.currentTerm, Success: false}

	// stale leader
	if args.Term < n.currentTerm {
		return reply
	}

	// Higher term or valid leader discovered
	if args.Term > n.currentTerm || n.state != Follower {
		n.becomeFollower(args.Term)
	} else {
		// same term, already Follower: then just reset the timer
		n.resetElectionTimer()
	}
	reply.Term = n.currentTerm

	// 1)log consistency check (PrevLog check)
	if args.PrevLogIndex > 0 {
		entry, ok := n.entryAt(args.PrevLogIndex)
		if !ok {
			//we dont have this index at all
			reply.ConflictIndex = uint64(len(n.raftLog))
			reply.ConflictTerm = 0
			return reply
		}
		if entry.Term != args.PrevLogTerm {
			//term mismatch, fast backward to first index of conflicting term
			reply.ConflictTerm = entry.Term
			reply.ConflictIndex = args.PrevLogIndex
			for reply.ConflictIndex > 1 {
				prevEntry, _ := n.entryAt(reply.ConflictIndex - 1)
				if prevEntry.Term != entry.Term {
					break
				}
				reply.ConflictIndex--
			}
			return reply
		}
	}

	// 2)conflict resolution and append
	for i, newEntry := range args.Entries {
		existingEntry, ok := n.entryAt(newEntry.Index)
		if ok && existingEntry.Term != newEntry.Term {
			//conflict:-truncate log from this index
			n.truncateFrom(newEntry.Index)
			ok = false //force append for this and remaining
		}
		if !ok {
			//append all remaining new entries
			for _, e := range args.Entries[i:] {
				n.appendEntry(e.Term, e.Command)
			}
			break
		}
	}

	//3)update commit index
	if args.LeaderCommit > n.commitIndex {
		lastNewIndex := n.lastIndex()
		if args.LeaderCommit < lastNewIndex {
			n.commitIndex = args.LeaderCommit
		} else {
			n.commitIndex = lastNewIndex
		}
		n.applyCommitted()
	}

	reply.Success = true
	return reply
}
