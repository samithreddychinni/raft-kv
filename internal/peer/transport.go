// transport: low-level TCP send/receive with gob encoding
package peer

import (
	"encoding/gob"
	"fmt"
	"net"
	"time"
)

type MsgType uint8

const (
	MsgPing MsgType = iota + 1
	MsgPong
)

//message is the only envelope exchanged between peers at this layer
type Message struct {
	Type MsgType
	From string // sender node "ID"
}

// listen accepts incoming connections and dispatches each to handleCon
func listen(addr, selfID string, onPing func(from string)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConn(conn, selfID, onPing)
	}
}

func handleConn(conn net.Conn, selfID string, onPing func(from string)) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	var msg Message
	if err := gob.NewDecoder(conn).Decode(&msg); err != nil {
		return
	}
	if msg.Type != MsgPing {
		return
	}
	onPing(msg.From)
	gob.NewEncoder(conn).Encode(Message{Type: MsgPong, From: selfID})
}

//dials addr, sends a ping, reads the pong, and closes the connection
func sendPing(addr, selfID string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	if err := gob.NewEncoder(conn).Encode(Message{Type: MsgPing, From: selfID}); err != nil {
		return err
	}
	var resp Message
	if err := gob.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Type != MsgPong {
		return fmt.Errorf("expected pong from %s, got %v", addr, resp.Type)
	}
	return nil
}
