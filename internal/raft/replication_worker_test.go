package raft

import (
	"bytes"
	"encoding/gob"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReplicationWorker_OneInFlightRPCAndConflictRecovery(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	var requests atomic.Int32
	var requestsMu sync.Mutex
	var appendRequests []AppendEntriesArgs
	recovered := make(chan struct{})

	n := makeNode("leader", []Peer{{ID: "follower", Addr: "follower:9100"}})
	n.appendEntriesRPC = func(_ string, _ uint8, body []byte, reply any) error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			seen := maxActive.Load()
			if current <= seen || maxActive.CompareAndSwap(seen, current) {
				break
			}
		}

		var args AppendEntriesArgs
		if err := gob.NewDecoder(bytes.NewReader(body)).Decode(&args); err != nil {
			return err
		}
		requestsMu.Lock()
		appendRequests = append(appendRequests, args)
		requestsMu.Unlock()

		attempt := requests.Add(1)
		appendReply := reply.(*AppendEntriesReply)
		if attempt == 1 {
			//keep the first RPC open across a few heartbeats.
			time.Sleep(3 * heartbeatInterval)
			*appendReply = AppendEntriesReply{Term: 1, ConflictIndex: 1}
			return nil
		}

		*appendReply = AppendEntriesReply{Term: 1, Success: true}
		if attempt == 2 {
			close(recovered)
		}
		return nil
	}
	n.mu.Lock()
	n.currentTerm = 1
	n.becomeLeader()
	n.mu.Unlock()
	t.Cleanup(func() {
		n.Stop()
		n.replicationWG.Wait()
	})

	select {
	case <-recovered:
	case <-time.After(2 * time.Second):
		t.Fatal("follower did not recover from conflict")
	}

	if got := maxActive.Load(); got != 1 {
		t.Fatalf("concurrent AppendEntries RPCs = %d; want 1", got)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(appendRequests) < 2 {
		t.Fatalf("AppendEntries requests = %d; want at least 2", len(appendRequests))
	}
	if appendRequests[0].PrevLogIndex != 1 {
		t.Fatalf("first PrevLogIndex = %d; want 1", appendRequests[0].PrevLogIndex)
	}
	if appendRequests[1].PrevLogIndex != 0 || len(appendRequests[1].Entries) != 1 {
		t.Fatalf("second request = prev=%d entries=%d; want prev=0 with one entry",
			appendRequests[1].PrevLogIndex, len(appendRequests[1].Entries))
	}
}
