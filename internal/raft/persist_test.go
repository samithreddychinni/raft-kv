// persist_test: crash-recovery tests for Raft persistence
package raft

import (
	"fmt"
	"os"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func newPersister(t *testing.T, dir, id string) *Persister {
	t.Helper()
	p, err := NewPersister(dir, id)
	if err != nil {
		t.Fatalf("NewPersister: %v", err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestHardState_RoundTrip(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	want := HardState{CurrentTerm: 7, VotedFor: "n2"}
	if err := p.SaveHardState(want); err != nil {
		t.Fatalf("SaveHardState: %v", err)
	}

	p2 := newPersister(t, dir, "n1")
	rs, err := p2.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if rs.HardState != want {
		t.Errorf("HardState = %+v; want %+v", rs.HardState, want)
	}
}

func TestHardState_EmptyVotedFor(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	want := HardState{CurrentTerm: 3, VotedFor: ""}
	if err := p.SaveHardState(want); err != nil {
		t.Fatalf("SaveHardState: %v", err)
	}

	p2 := newPersister(t, dir, "n1")
	rs, err := p2.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if rs.HardState != want {
		t.Errorf("HardState = %+v; want %+v", rs.HardState, want)
	}
}

func TestHardState_FirstBoot_ZeroValue(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")
	rs, err := p.LoadState()
	if err != nil {
		t.Fatalf("LoadState on fresh node: %v", err)
	}
	if rs.HardState.CurrentTerm != 0 || rs.HardState.VotedFor != "" {
		t.Errorf("expected zero HardState, got %+v", rs.HardState)
	}
	if len(rs.Log) != 0 {
		t.Errorf("expected empty log, got %d entries", len(rs.Log))
	}
}

func TestHardState_Overwrite(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	if err := p.SaveHardState(HardState{CurrentTerm: 1, VotedFor: "n2"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveHardState(HardState{CurrentTerm: 2, VotedFor: "n3"}); err != nil {
		t.Fatal(err)
	}

	p2 := newPersister(t, dir, "n1")
	rs, err := p2.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	want := HardState{CurrentTerm: 2, VotedFor: "n3"}
	if rs.HardState != want {
		t.Errorf("HardState = %+v; want %+v", rs.HardState, want)
	}
}

func TestLog_AppendAndRecover(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	entries := []LogEntry{
		{Index: 1, Term: 1, Command: []byte("set foo bar")},
		{Index: 2, Term: 1, Command: []byte("set baz qux")},
		{Index: 3, Term: 2, Command: []byte("del foo")},
	}
	for _, e := range entries {
		if err := p.AppendLogEntry(e); err != nil {
			t.Fatalf("AppendLogEntry idx=%d: %v", e.Index, err)
		}
	}

	p2 := newPersister(t, dir, "n1")
	rs, err := p2.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if len(rs.Log) != len(entries) {
		t.Fatalf("recovered %d entries; want %d", len(rs.Log), len(entries))
	}
	for i, want := range entries {
		got := rs.Log[i]
		if got.Index != want.Index || got.Term != want.Term || string(got.Command) != string(want.Command) {
			t.Errorf("[%d] got %+v; want %+v", i, got, want)
		}
	}
}

func TestLog_EmptyCommand(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	if err := p.AppendLogEntry(LogEntry{Index: 1, Term: 1, Command: nil}); err != nil {
		t.Fatal(err)
	}

	p2 := newPersister(t, dir, "n1")
	rs, err := p2.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Log) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rs.Log))
	}
	if len(rs.Log[0].Command) != 0 {
		t.Errorf("command should be empty, got %q", rs.Log[0].Command)
	}
}

func TestLog_CorruptTailTruncated(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	good := []LogEntry{
		{Index: 1, Term: 1, Command: []byte("cmd1")},
		{Index: 2, Term: 1, Command: []byte("cmd2")},
	}
	for _, e := range good {
		if err := p.AppendLogEntry(e); err != nil {
			t.Fatal(err)
		}
	}
	p.Close()

	path := fmt.Sprintf("%s/n1.raft.log", dir)
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := f.Stat()
	corrupt := make([]byte, 8)
	for i := range corrupt {
		corrupt[i] = 0xFF
	}
	f.WriteAt(corrupt, info.Size()-8) // corrupt the final record's CRC
	f.Close()

	p2, err := NewPersister(dir, "n1")
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()

	rs, err := p2.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Log) != 1 {
		t.Fatalf("expected 1 good entry after corruption, got %d", len(rs.Log))
	}
	if rs.Log[0].Index != 1 {
		t.Errorf("recovered entry index = %d; want 1", rs.Log[0].Index)
	}

	if err := p2.AppendLogEntry(LogEntry{Index: 2, Term: 2, Command: []byte("retry")}); err != nil {
		t.Fatalf("AppendLogEntry after corruption recovery: %v", err)
	}
}

func TestLog_TruncateFrom(t *testing.T) {
	dir := tempDir(t)
	p := newPersister(t, dir, "n1")

	for i := 1; i <= 5; i++ {
		if err := p.AppendLogEntry(LogEntry{
			Index:   uint64(i),
			Term:    1,
			Command: []byte(fmt.Sprintf("cmd%d", i)),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := p.TruncateLogFrom(3); err != nil {
		t.Fatalf("TruncateLogFrom: %v", err)
	}

	if err := p.AppendLogEntry(LogEntry{Index: 3, Term: 2, Command: []byte("new3")}); err != nil {
		t.Fatal(err)
	}

	p2 := newPersister(t, dir, "n1")
	rs, err := p2.LoadState()
	if err != nil {
		t.Fatal(err)
	}

	if len(rs.Log) != 3 {
		t.Fatalf("expected 3 entries (1,2,new3), got %d", len(rs.Log))
	}
	if rs.Log[2].Term != 2 || string(rs.Log[2].Command) != "new3" {
		t.Errorf("entry[2] = %+v; want idx=3 term=2 cmd=new3", rs.Log[2])
	}
}

// end-to-end: win election, append, "crash", restart, verify state survived
func TestRaftNode_PersistenceSurvivesRestart(t *testing.T) {
	dir := tempDir(t)
	id := "n1"

	n := NewRaftNode(id, ":0", nil, dir)
	n.mu.Lock()
	n.startElection()
	n.mu.Unlock()

	n.mu.Lock()
	n.appendEntry(1, []byte("set x 1"))
	n.appendEntry(1, []byte("set y 2"))
	n.mu.Unlock()

	n.mu.Lock()
	wantTerm := n.currentTerm
	wantVoted := n.votedFor
	wantLastIdx := n.lastLogIndex
	n.mu.Unlock()

	n.persister.Close() // abrupt shutdown, no Stop()

	n2 := NewRaftNode(id, ":0", nil, dir)

	n2.mu.Lock()
	gotTerm := n2.currentTerm
	gotVoted := n2.votedFor
	gotLastIdx := n2.lastLogIndex
	n2.mu.Unlock()

	if gotTerm != wantTerm {
		t.Errorf("term: got %d, want %d", gotTerm, wantTerm)
	}
	if gotVoted != wantVoted {
		t.Errorf("votedFor: got %q, want %q", gotVoted, wantVoted)
	}
	if gotLastIdx != wantLastIdx {
		t.Errorf("lastLogIndex: got %d, want %d", gotLastIdx, wantLastIdx)
	}
	if int(gotLastIdx)+1 != len(n2.raftLog) {
		t.Errorf("raftLog len mismatch: lastIdx=%d but len(log)=%d", gotLastIdx, len(n2.raftLog))
	}
}
