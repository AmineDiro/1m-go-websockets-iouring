package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/iceber/iouring-go"
	"github.com/sirupsen/logrus"
)

const readSize int = 1024

type WSRing struct {
	iouring    *iouring.IOURing
	resultChan chan iouring.Result
	mu         *sync.RWMutex
	conns      map[int]net.Conn
}

func MkRing(entries uint) (*WSRing, error) {
	ring, err := iouring.New(entries)
	if err != nil {
		return nil, err
	}
	return &WSRing{
		iouring:    ring,
		resultChan: make(chan iouring.Result),
		conns:      make(map[int]net.Conn),
		mu:         &sync.RWMutex{},
	}, nil
}

// Reads from the io_uring CQ
func (wr *WSRing) Loop() {
	log.Println("Starting CQ loop")
	for {
		result := <-wr.resultChan
		switch result.Opcode() {

		case iouring.OpRead:
			wr.read(result)

		case iouring.OpClose:
			wr.close(result)
		}
	}
}

func (wr *WSRing) Add(conn net.Conn) error {

	wr.mu.Lock()
	defer wr.mu.Unlock()

	fd := websocketFD(conn)

	wr.conns[fd] = conn

	buffer := make([]byte, readSize)
	prep := iouring.Read(fd, buffer)
	if _, err := wr.iouring.SubmitRequest(prep, wr.resultChan); err != nil {
		log.Fatal("Ring add error", err)
		return err
	}
	return nil
}

func (wr *WSRing) read(result iouring.Result) {
	wr.mu.RLock()
	defer wr.mu.RUnlock()

	num := result.ReturnValue0().(int)
	err := result.Err()

	logrus.WithFields(logrus.Fields{
		"id":          result.Fd(),
		"Connection":  wr.conns[result.Fd()].RemoteAddr(),
		"bytesToRead": num,
		"value1":      result.ReturnValue1(),
	}).Info("Result")

	if err != nil {
		log.Fatal("Read error", err)
	}

	buffer, _ := result.GetRequestBuffer()
	content := buffer[:num]

	// TODO : Parse Message
	r := bytes.NewReader(content)
	msg := []wsutil.Message{}
	msg, _ = wsutil.ReadMessage(r, ws.StateServerSide, msg)
	fmt.Printf("Content: %#v\n", msg)

	// This could be slow :
	// Allocate buffer on each read !!
	// TODO: Reuse the same buffer
	// buffer := make([]byte, readSize)
	prep := iouring.Read(result.Fd(), buffer)
	if _, err := wr.iouring.SubmitRequest(prep, wr.resultChan); err != nil {
		panic(err)
	}
}

func (wr *WSRing) close(result iouring.Result) {
	clientAddr := result.GetRequestInfo().(string)
	if err := result.Err(); err != nil {
		panic(err)
	}
	connPrintf(clientAddr, "close successful\n")
}

func websocketFD(conn net.Conn) int {
	tcpConn := reflect.Indirect(reflect.ValueOf(conn)).FieldByName("conn")
	fdVal := tcpConn.FieldByName("fd")
	pfdVal := reflect.Indirect(fdVal).FieldByName("pfd")

	return int(pfdVal.FieldByName("Sysfd").Int())
}

func connPrintf(addr string, format string, a ...interface{}) {
	prefix := fmt.Sprintf("[%s]", addr)
	fmt.Printf(prefix+format, a...)
}
