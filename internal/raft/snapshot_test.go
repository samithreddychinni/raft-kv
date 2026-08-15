package raft

import (
	"bytes"
	"encoding/gob"
	"testing"
)

func TestCompact_PersistsSnapshotAndLogTail(t *testing.T) {
	dir := t.TempDir()
	n := NewRaftNode("n1", ":0", nil, dir)
	t.Cleanup(n.Stop)

	n.mu.Lock()
	n.appendEntry(1, []byte("one"))
	n.appendEntry(1, []byte("two"))
	n.appendEntry(2, []byte("three"))
	n.commitIndex = 3
	n.lastApplied = 3
	n.Applied(3)
	n.mu.Unlock()

	data := []byte(`{"key":"value"}`)
	if err := n.Compact(2, data); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	n.mu.Lock()
	if n.raftLog[0].Index != 2 || n.raftLog[0].Term != 1 {
		t.Fatalf("snapshot base = %+v; want index 2, term 1", n.raftLog[0])
	}
	if _, ok := n.entryAt(1); ok {
		t.Fatal("compacted entry remains available")
	}
	n.mu.Unlock()

	n.Stop()
	if err := n.persister.Close(); err != nil {
		t.Fatal(err)
	}
	var restored []byte
	n2 := NewRaftNodeWithSnapshot("n1", ":0", nil, dir, func(got []byte) error {
		restored = append([]byte(nil), got...)
		return nil
	})
	t.Cleanup(n2.Stop)
	if !bytes.Equal(restored, data) {
		t.Fatalf("restored snapshot = %q; want %q", restored, data)
	}
	n2.mu.Lock()
	defer n2.mu.Unlock()
	if n2.lastIndex() != 3 || n2.lastTerm() != 2 {
		t.Fatalf("recovered tail ends at (%d, %d); want (3, 2)", n2.lastIndex(), n2.lastTerm())
	}
}

func TestSendAppendEntries_SendsSnapshotToLaggingFollower(t *testing.T) {
	n := makeNode("leader", []Peer{{ID: "follower", Addr: "follower:9100"}})
	n.mu.Lock()
	n.state = Leader
	n.currentTerm = 3
	n.snapshot = Snapshot{LastIncludedIndex: 5, LastIncludedTerm: 2, Data: []byte("state")}
	n.raftLog = []LogEntry{{Index: 5, Term: 2}}
	n.lastLogIndex, n.lastLogTerm = 5, 2
	n.nextIndex = map[string]uint64{"follower": 1}
	n.matchIndex = map[string]uint64{"follower": 0}
	n.mu.Unlock()

	var got InstallSnapshotArgs
	n.installSnapshotRPC = func(_ string, _ uint8, body []byte, reply any) error {
		if err := gob.NewDecoder(bytes.NewReader(body)).Decode(&got); err != nil {
			return err
		}
		*reply.(*InstallSnapshotReply) = InstallSnapshotReply{Term: 3, Success: true}
		return nil
	}

	n.sendAppendEntries(n.peers[0], 3)
	if got.LastIncludedIndex != 5 || got.LastIncludedTerm != 2 || string(got.Data) != "state" {
		t.Fatalf("snapshot sent = %+v; want index 5, term 2, state", got)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.matchIndex["follower"] != 5 || n.nextIndex["follower"] != 6 {
		t.Fatalf("follower progress = (%d, %d); want (5, 6)", n.matchIndex["follower"], n.nextIndex["follower"])
	}
}

func TestHandleInstallSnapshot_RestoresStateAndKeepsMatchingTail(t *testing.T) {
	var restored []byte
	n := NewRaftNodeWithSnapshot("follower", ":0", nil, "", func(data []byte) error {
		restored = append([]byte(nil), data...)
		return nil
	})
	t.Cleanup(n.Stop)
	n.mu.Lock()
	n.currentTerm = 4
	n.appendEntry(3, []byte("one"))
	n.appendEntry(4, []byte("two"))
	n.appendEntry(4, []byte("three"))
	n.lastApplied = 3
	n.Applied(3)
	n.mu.Unlock()

	reply := n.HandleInstallSnapshot(InstallSnapshotArgs{
		Term:              4,
		LeaderID:          "leader",
		LastIncludedIndex: 2,
		LastIncludedTerm:  4,
		Data:              []byte("state"),
	})
	if !reply.Success {
		t.Fatalf("InstallSnapshot reply = %+v; want success", reply)
	}
	if string(restored) != "state" {
		t.Fatalf("restored state = %q; want state", restored)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.raftLog[0].Index != 2 || n.lastIndex() != 3 {
		t.Fatalf("log range = %d..%d; want 2..3", n.raftLog[0].Index, n.lastIndex())
	}
}
