// peer: node identity, live/dead state tracking, and ping loops
package peer

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	pingInterval = 2 * time.Second
	dialTimeout  = 1 * time.Second
)

type status int

const (
	statusUnknown status = iota
	statusAlive
	statusDead
)

func (s status) String() string {
	switch s {
	case statusAlive:
		return "alive"
	case statusDead:
		return "dead"
	default:
		return "unknown"
	}
}

//peer holds the identity and address of a remote node
type Peer struct {
	ID   string
	Addr string
}

type peerState struct {
	status   status
	lastSeen time.Time
}

//represents this process in the cluster
type Node struct {
	ID   string
	Addr string // TCP address this node listens on for peer traffic

	peers  []Peer
	mu     sync.RWMutex
	states map[string]*peerState
}

func NewNode(id, addr string, peers []Peer) *Node {
	states := make(map[string]*peerState, len(peers))
	for _, p := range peers {
		states[p.ID] = &peerState{status: statusUnknown}
	}
	return &Node{ID: id, Addr: addr, peers: peers, states: states}
}

// Start opens the TCP listener and launches ping goroutines for every peer
func (n *Node) Start() {
	go func() {
		if err := listen(n.Addr, n.ID, func(from string) {
			log.Printf("[%s] ping from %s", n.ID, from)
		}); err != nil {
			log.Printf("[%s] peer listener error: %v", n.ID, err)
		}
	}()

	for _, p := range n.peers {
		go n.pingLoop(p)
	}
	go n.printStatusLoop()
}

func (n *Node) markAlive(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok := n.states[id]; ok {
		s.status = statusAlive
		s.lastSeen = time.Now()
	}
}

func (n *Node) markDead(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok := n.states[id]; ok {
		s.status = statusDead
	}
}

func (n *Node) pingLoop(p Peer) {
	for {
		if err := sendPing(p.Addr, n.ID, dialTimeout); err != nil {
			n.markDead(p.ID)
		} else {
			n.markAlive(p.ID)
		}
		time.Sleep(pingInterval)
	}
}

func (n *Node) printStatusLoop() {
	for {
		time.Sleep(3 * time.Second)
		n.mu.RLock()
		fmt.Printf("\n[%s] peer status:\n", n.ID)
		for _, p := range n.peers {
			s := n.states[p.ID]
			if s.status == statusAlive {
				fmt.Printf("  %-10s  %s  [last seen %s ago]\n",
					p.ID, s.status, time.Since(s.lastSeen).Round(time.Millisecond))
			} else {
				fmt.Printf("  %-10s  %s\n", p.ID, s.status)
			}
		}
		n.mu.RUnlock()
	}
}
