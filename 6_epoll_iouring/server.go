package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"syscall"

	"github.com/gobwas/ws"
	"github.com/iceber/iouring-go"
)

var epoller *epoll
var ring *WSRing

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}
	if err := epoller.Add(conn); err != nil {
		log.Printf("Failed to add connection %v", err)
		conn.Close()
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

	go func() {
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			log.Fatalf("pprof failed: %v", err)
		}
	}()

	// Start epoll
	var err error
	epoller, err = MkEpoll()
	if err != nil {
		panic(err)
	}
	go Start()

	// Start Ring
	ring, _ = MkRing(epoller, 8024, 1000)
	go ring.Loop()

	http.HandleFunc("/", wsHandler)
	if err := http.ListenAndServe("0.0.0.0:8000", nil); err != nil {
		log.Fatal(err)
	}
}

func Start() {
	for {
		connections, err := epoller.Wait()
		if err != nil {
			log.Printf("Failed to epoll wait %v", err)
			continue
		}
		// These are the "ready-to-read" connections
		for _, conn := range connections {
			if conn == nil {
				break
			}
			fd := websocketFD(conn)
			buffer := <-ring.memPool
			prep := iouring.Read(fd, *buffer)
			if _, err := ring.iouring.SubmitRequest(prep, ring.resultChan); err != nil {
				log.Fatal("Ring add error", err)
				conn.Close()
			}
		}
	}
}
