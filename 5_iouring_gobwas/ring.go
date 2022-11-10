package main

import (
	"fmt"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/iceber/iouring-go"
)

const readSize int = 64

type WSRing struct {
	iouring    *iouring.IOURing
	resultChan chan iouring.Result
	mu         *sync.RWMutex
	conns      map[int]net.Conn
	memPool    chan *[]byte
}

func MkRing(entries uint, poolSize uint) (*WSRing, error) {
	ring, err := iouring.New(entries)
	if err != nil {
		return nil, err
	}
	pool, _ := seedPool(poolSize)
	return &WSRing{
		iouring:    ring,
		resultChan: make(chan iouring.Result),
		conns:      make(map[int]net.Conn),
		memPool:    pool,
		mu:         &sync.RWMutex{},
	}, nil
}

func seedPool(poolSize uint) (chan *[]byte, error) {
	pool := make(chan *[]byte, poolSize)
	for i := 0; i < int(poolSize); i++ {
		buf := make([]byte, readSize)
		pool <- &buf
	}
	return pool, nil
}

// Reads from the io_uring CQ
func (wr *WSRing) Loop() {
	for result := range wr.resultChan {
		switch result.Opcode() {
		case iouring.OpRead:
			wr.Read(result)

		case iouring.OpClose:
			wr.close(result)
		}
	}
}

func (wr *WSRing) Add(conn net.Conn) error {

	fd := websocketFD(conn)

	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.conns[fd] = conn
	if len(wr.conns)%100 == 0 {
		log.Printf("Total number of connections: %v", len(wr.conns))
	}

	buffer := <-wr.memPool
	prep := iouring.Read(fd, *buffer)
	if _, err := wr.iouring.SubmitRequest(prep, wr.resultChan); err != nil {
		log.Fatal("Ring add error", err)
		return err
	}
	return nil
}

func (wr *WSRing) Read(result iouring.Result) {
	err := result.Err()
	buffer, _ := result.GetRequestBuffer()
	wr.memPool <- &buffer

	// num := result.ReturnValue0().(int)
	// content := buffer[:num]

	// TODO : Parse Message
	// r := bytes.NewReader(content)
	// msg := []wsutil.Message{}
	// msg, _ = wsutil.ReadMessage(r, ws.StateServerSide, msg)

	// logrus.WithFields(logrus.Fields{
	// 	"id":          result.Fd(),
	// 	"Connection":  wr.conns[result.Fd()].RemoteAddr(),
	// 	"bytesToRead": num,
	// 	// "msg":         string(msg),
	// }).Info("Read Result")

	if err != nil {
		log.Fatal("Read error", err)
	}

	// prep := iouring.Read(result.Fd(), buffer)
	// if _, err := wr.iouring.SubmitRequest(prep, wr.resultChan); err != nil {
	// 	panic(err)
	// }
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
