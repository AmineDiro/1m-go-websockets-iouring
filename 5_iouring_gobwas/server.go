package main

import (
	"fmt"
	"net"
	"reflect"
	"syscall"

	"github.com/iceber/iouring-go"
)

const readSize = 1024

type WSRing struct {
	ring     *iouring.IOURing
	ringChan chan iouring.Result
}

func MkRing(entries uint) (*WSRing, error) {
	ring, err := iouring.New(entries)
	if err != nil {
		return nil, err
	}
	return &WSRing{
		ring:     ring,
		ringChan: make(chan iouring.Result),
	}, nil
}
func (wr *WSRing) Loop() {
	for {
		result := <-wr.ringChan
		switch result.Opcode() {
		case iouring.OpAccept:
			// if _, err := iour.SubmitRequest(iouring.Accept(fd), resulter); err != nil {
			// 	panicf("submit accept request error: %v", err)
			// }
			wr.accept(result)

		case iouring.OpRead:
			wr.read(result)

		case iouring.OpWrite:
			wr.write(result)

		case iouring.OpClose:
			wr.close(result)
		}
	}
}

func websocketFD(conn net.Conn) int {
	tcpConn := reflect.Indirect(reflect.ValueOf(conn)).FieldByName("conn")
	fdVal := tcpConn.FieldByName("fd")
	pfdVal := reflect.Indirect(fdVal).FieldByName("pfd")

	return int(pfdVal.FieldByName("Sysfd").Int())
}

func (wr *WSRing) read(result iouring.Result) {
	clientAddr := result.GetRequestInfo().(string)
	if err := result.Err(); err != nil {
		panicf("[%s] read error: %v", clientAddr, err)
	}

	num := result.ReturnValue0().(int)
	buf, _ := result.GetRequestBuffer()
	content := buf[:num]

	connPrintf(clientAddr, "read byte: %v\ncontent: %s\n", num, content)

	buffer := make([]byte, readSize)
	prep := iouring.Read(result.Fd(), buffer).WithInfo(clientAddr)
	if _, err := iour.SubmitRequest(prep, resulter); err != nil {
		panicf("submit read request error: %v", err)
	}
	// prep := iouring.Write(result.Fd(), content).WithInfo(clientAddr)
	// if _, err := iour.SubmitRequest(prep, resulter); err != nil {
	// 	panicf("[%s] submit write request error: %v", clientAddr, err)
	// }
}

func (wr *WSRing) write(result iouring.Result) {
	clientAddr := result.GetRequestInfo().(string)
	if err := result.Err(); err != nil {
		panicf("[%s] write error: %v", clientAddr, err)
	}
	connPrintf(clientAddr, "write successful\n")

	// prep := iouring.Close(result.Fd()).WithInfo(clientAddr)
	// if _, err := iour.SubmitRequest(prep, resulter); err != nil {
	// 	panicf("[%s] submit write request error: %v", clientAddr, err)
	// }
}

func close(result iouring.Result) {
	clientAddr := result.GetRequestInfo().(string)
	if err := result.Err(); err != nil {
		panicf("[%s] close error: %v", clientAddr, err)
	}
	connPrintf(clientAddr, "close successful\n")
}

func listenSocket(addr string) int {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		panic(err)
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		panic(err)
	}

	sockaddr := &syscall.SockaddrInet4{Port: tcpAddr.Port}
	copy(sockaddr.Addr[:], tcpAddr.IP.To4())
	if err := syscall.Bind(fd, sockaddr); err != nil {
		panic(err)
	}

	if err := syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		panic(err)
	}
	return fd
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}

func connPrintf(addr string, format string, a ...interface{}) {
	prefix := fmt.Sprintf("[%s]", addr)
	fmt.Printf(prefix+format, a...)
}
