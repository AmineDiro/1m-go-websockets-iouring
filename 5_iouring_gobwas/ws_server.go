package main

import (
	"log"
	"net/http"
	"syscall"

	"github.com/gobwas/ws"
	"github.com/iceber/iouring-go"
)

var ring *WSRing

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}
	// Add the fd to ring
	fd := websocketFD(conn)
	if _, err := iour.SubmitRequest(iouring.Accept(fd), ioringChan); err != nil {
		panicf("submit accept request error: %v", err)
	}
}

func main() {
	// Increase resources limitations
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}
	rLimit.Cur = rLimit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}

	// Creating Ring and starting
	ring, err := MkRing(1024)
	if err != nil {
		panic(err)
	}
	go ring.Loop()
	// Start WS Server

	http.HandleFunc("/", wsHandler)
	if err := http.ListenAndServe("0.0.0.0:8000", nil); err != nil {
		log.Fatal(err)
	}
}
