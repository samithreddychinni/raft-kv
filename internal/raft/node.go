// node: raftNode is the core state machine
// fields and methods map 1:1 to the state machine diagram transitions
package raft

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

const (
	//electionTimeoutMin/Max define the randomised window
	//randomisation prevents split votes
	electionTimeoutMin = 150 * time.Millisecond
	electionTimeoutMax = 300 * time.Millisecond

	//heartbeatInterval must be well below the minimum election timeout
	heartbeatInterval = 50 * time.Millisecond

	dialTimeout = 500 * time.Millisecond
)

//peer is a remote node the raftnode communicates with
type Peer struct {
	ID   string
	Addr string
}

//raftnode is the state machine. All exported methods are safe for concurrent use
type RaftNode struct {
	mu sync.Mutex

	//identification values for node
	id   string
	addr string
	peers []Peer

	// persistent state (must survive restarts, simplified for now)
	currentTerm uint64
	votedFor    string // if "" means not voted in currentTerm

	// 	volatile state
	state         RaftState
	votes         int
	electionTimer *time.Timer

	// log metadata — updated on every append; used for the vote check.
	// both are 0 while the log is empty (election-only phase).
	lastLogIndex uint64
	lastLogTerm  uint64

	// in-memory Raft log; index 0 is a zero-entry sentinel
	raftLog []LogEntry

	// replicated state machine indices
	commitIndex uint64 // highest index known committed
	lastApplied uint64 // highest index applied to the state machine

	// apply channel: committed entries flow here for the store to consume
	applyCh chan LogEntry

	// leader-only: per-peer replication bookkeeping
	nextIndex  map[string]uint64 // next log index to send to each peer
	matchIndex map[string]uint64 // highest index confirmed replicated on each peer

	// current known leader (may be empty if unknown)
	leaderID string
}

//NewRaftNode creates a raftnode that starts as a follower
func NewRaftNode(id, addr string, peers []Peer) *RaftNode {
	n := &RaftNode{
		id:    id,
		addr:  addr,
		peers: peers,
		state: Follower,
		applyCh: make(chan LogEntry, 256),
	}
	n.initLog()
	
	//timer fires once then We restart it with a new random duration every time
	n.electionTimer = time.AfterFunc(n.randomTimeout(), n.onElectionTimeout)
	return n
}

// Start opens the RPC listener
func (n *RaftNode) Start() {
	go n.listenRPC()
}

// Stop cancels the election timer
func (n *RaftNode) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.electionTimer.Stop()
}

// State returns the current RaftState (for logging/testing will remove in future)
func (n *RaftNode) State() RaftState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state
}

// Term returns the current term (for testing will remove in future)
func (n *RaftNode) Term() uint64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.currentTerm
}

// IsLeader answers the qn "whether this node is the current leader ?"
func (n *RaftNode) IsLeader() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state == Leader
}

// LeaderAddr returns the Raft RPC address of the current known leader
// returns "" if this node is the leader or the leader is unknown
func (n *RaftNode) LeaderAddr() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state == Leader {
		return "" // I am the leader
	}
	return n.leaderID
}

// ApplyCh returns the channel on which committed log entries are delivered
// the caller (main.go) must drain this channel continuously
func (n *RaftNode) ApplyCh() <-chan LogEntry {
	return n.applyCh
}

// ErrNotLeader is returned by propose when this node is not the leader
var ErrNotLeader = fmt.Errorf("raft: not leader")

// propose submits a command to the Raft log
// blocks until the entry is committed and applied, then returns
// returns ErrNotLeader if this node is not the leader
func (n *RaftNode) Propose(cmd []byte) error {
	n.mu.Lock()
	if n.state != Leader {
		n.mu.Unlock()
		return ErrNotLeader
	}
	idx := n.appendEntry(n.currentTerm, cmd)
	n.mu.Unlock()

	// wait until the entry at idx is applied to the state machine
	// apply loop signals via applyCh we watch commitIndex instead
	// of a per index channel to stay simple
	for {
		n.mu.Lock()
		applied := n.lastApplied
		n.mu.Unlock()
		if applied >= idx {
			return nil
		}
		// small sleep to yield a production impl would use a cond var or per index channel
		time.Sleep(1 * time.Millisecond)
	}
}

//randomTimeout returns a duration in [electionTimeoutMin, electionTimeoutMax)
//called every time the timer resets to avoid coordinated split votes 
//[look at https://raft.github.io/raft.pdf Section 5.2 to learn more]
func (n *RaftNode) randomTimeout() time.Duration {
	delta := electionTimeoutMax - electionTimeoutMin
	return electionTimeoutMin + time.Duration(rand.Int63n(int64(delta)))
}

//resetElectionTimer resets the timer with a new random duration.
//must be called with n.mu held.
func (n *RaftNode) resetElectionTimer() {
	n.electionTimer.Stop()
	n.electionTimer.Reset(n.randomTimeout())
}

// becomeFollower transitions this node to Follower
//   1)Candidate to Follower (discovers leader OR higher term)
//   2) Leader to Follower (discovers higher term)
// must be called with n.mu held
func (n *RaftNode) becomeFollower(term uint64) {
	n.state = Follower
	n.currentTerm = term
	n.votedFor = ""
	n.resetElectionTimer()
	log.Printf("[%s] → Follower  term=%d", n.id, n.currentTerm)
}

// onElectionTimeout is fired by the election timer goroutine
//Follower to Candidate (election timeout)
func (n *RaftNode) onElectionTimeout() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state == Leader {
		//Leaders never time out into candidates
		return
	}
	n.startElection()
}

//RPC transport helpers

// encode gob encodes v into a []byte.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sendRPC dials addr, sends msgType + body, reads a response body, and decodes into reply
func sendRPC(addr string, msgType uint8, body []byte, reply any) error {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(dialTimeout))

	enc := gob.NewEncoder(conn)
	dec := gob.NewDecoder(conn)

	//Frame:-[msgType uint8] [body gob-bytes as []byte field in envelope]
	env := envelope{Type: msgType, Body: body}
	if err := enc.Encode(env); err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}

	var respEnv envelope
	if err := dec.Decode(&respEnv); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return gob.NewDecoder(bytes.NewReader(respEnv.Body)).Decode(reply)
}

//envelope is our minimal transport frame
type envelope struct {
	Type uint8
	Body []byte
}

// listenRPC accepts inbound RPC connections on n.addr
func (n *RaftNode) listenRPC() {
	ln, err := net.Listen("tcp", n.addr)
	if err != nil {
		log.Printf("[%s] raft listener error: %v", n.id, err)
		return
	}
	log.Printf("[%s] raft RPC listening on %s", n.id, n.addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go n.handleConn(conn)
	}
}

// handleConn reads one envelope, dispatches to the correct handler, writes the reply
func (n *RaftNode) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(dialTimeout))

	enc := gob.NewEncoder(conn)
	dec := gob.NewDecoder(conn)

	var env envelope
	if err := dec.Decode(&env); err != nil {
		return
	}

	var replyBody []byte
	switch env.Type {
	case MsgRequestVote:
		var args RequestVoteArgs
		if err := gob.NewDecoder(bytes.NewReader(env.Body)).Decode(&args); err != nil {
			return
		}
		reply := n.HandleRequestVote(args)
		replyBody, _ = encode(reply)
		enc.Encode(envelope{Type: MsgRequestVoteReply, Body: replyBody})

	case MsgAppendEntries:
		var args AppendEntriesArgs
		if err := gob.NewDecoder(bytes.NewReader(env.Body)).Decode(&args); err != nil {
			return
		}
		reply := n.HandleAppendEntries(args)
		replyBody, _ = encode(reply)
		enc.Encode(envelope{Type: MsgAppendEntriesReply, Body: replyBody})
	}
}
