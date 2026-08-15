package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/samithreddychinni/raftkv/internal/peer"
	"github.com/samithreddychinni/raftkv/internal/raft"
	"github.com/samithreddychinni/raftkv/internal/store"
	"github.com/samithreddychinni/raftkv/server"
)

const snapshotInterval = 1024

func main() {
	id := flag.String("id", "node1", "unique node ID")
	httpAddr := flag.String("http", ":8080", "HTTP listen address")
	peerAddr := flag.String("peer-addr", ":9001", "TCP address for peer traffic")
	raftAddr := flag.String("raft-addr", ":9101", "TCP address for Raft RPC traffic")
	peersFlag := flag.String("peers", "", "peer list: id=peerAddr=raftAddr,...")
	dataDir := flag.String("data-dir", "data", "directory for durable Raft state (*.raft.meta, *.raft.log)")
	flag.Parse()

	var peerNodes []peer.Peer
	var raftPeers []raft.Peer

	if *peersFlag != "" {
		for _, entry := range strings.Split(*peersFlag, ",") {
			// Format: id=peerAddr=raftAddr
			parts := strings.SplitN(entry, "=", 3)
			if len(parts) != 3 {
				fmt.Fprintf(os.Stderr, "invalid peer entry (want id=peerAddr=raftAddr): %q\n", entry)
				os.Exit(1)
			}
			peerNodes = append(peerNodes, peer.Peer{ID: parts[0], Addr: parts[1]})
			raftPeers = append(raftPeers, raft.Peer{ID: parts[0], Addr: parts[2]})
		}
	}

	// start peer node (ping/pong health tracking)
	n := peer.NewNode(*id, *peerAddr, peerNodes)
	n.Start()

	// start key-value store backed by WAL
	walPath := *id + ".wal"
	s, err := store.NewStoreFromWAL(walPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Restore the store snapshot before the Raft node accepts RPCs.
	rn := raft.NewRaftNodeWithSnapshot(*id, *raftAddr, raftPeers, *dataDir, s.RestoreSnapshot)
	rn.Start()

	srv := server.NewServer(s, rn)
	lastSnapshotIndex := rn.SnapshotIndex()

	// apply loop: drains committed Raft entries and applies them to the store.
	// runs on both leader and follower same code path.
	go func() {
		for entry := range rn.ApplyCh() {
			if err := s.Apply(entry); err != nil {
				fmt.Fprintf(os.Stderr, "apply error: %v\n", err)
				continue
			}
			rn.Applied(entry.Index)
			if entry.Index-lastSnapshotIndex >= snapshotInterval {
				data, err := s.Snapshot()
				if err != nil {
					fmt.Fprintf(os.Stderr, "snapshot error: %v\n", err)
					continue
				}
				if err := rn.Compact(entry.Index, data); err != nil {
					fmt.Fprintf(os.Stderr, "compact error: %v\n", err)
				} else {
					lastSnapshotIndex = entry.Index
				}
			}
		}
	}()

	fmt.Printf("[%s] HTTP=%s  peer=%s  raft=%s\n", *id, *httpAddr, *peerAddr, *raftAddr)
	if err := http.ListenAndServe(*httpAddr, srv); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
	}
}
