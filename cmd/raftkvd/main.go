package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/samithreddychinni/raftkv/internal/peer"
	"github.com/samithreddychinni/raftkv/internal/store"
	"github.com/samithreddychinni/raftkv/server"
)

func main() {
	id := flag.String("id", "node1", "unique node ID")
	httpAddr := flag.String("http", ":8080", "HTTP listen address")
	peerAddr := flag.String("peer-addr", ":9001", "TCP address for peer 2 peer traffic")
	peersFlag := flag.String("peers", "", "peers: id=addr,id=addr")
	flag.Parse()

	//parse peer list
	var peers []peer.Peer
	if *peersFlag != "" {
		for _, entry := range strings.Split(*peersFlag, ",") {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "invalid peer entry: %q\n", entry)
				os.Exit(1)
			}
			peers = append(peers, peer.Peer{ID: parts[0], Addr: parts[1]})
		}
	}

	//start peer node (listener + ping goroutines)
	n := peer.NewNode(*id, *peerAddr, peers)
	n.Start()
	// each node gets one wal file for itself
	walPath := *id + ".wal"
	s, err := store.NewStoreFromWAL(walPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	srv := server.NewServer(s)
	fmt.Printf("[%s] HTTP on %s | peer TCP on %s\n", *id, *httpAddr, *peerAddr)
	if err := http.ListenAndServe(*httpAddr, srv); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
	}
}
