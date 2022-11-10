package main

import (
	"bytes"
	"io"
	"io/ioutil"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/iceber/iouring-go"
	"github.com/sirupsen/logrus"
)

const readSize int = 64

type WSRing struct {
	iouring    *iouring.IOURing
	resultChan chan iouring.Result
	memPool    chan *[]byte
	epoller    *epoll
}

func MkRing(epoller *epoll, entries uint, poolSize uint) (*WSRing, error) {
	ring, err := iouring.New(entries)
	if err != nil {
		return nil, err
	}
	pool, _ := seedPool(poolSize)
	return &WSRing{
		iouring:    ring,
		resultChan: make(chan iouring.Result),
		memPool:    pool,
		epoller:    epoller,
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
			err := wr.Read(result)
			// Signal to epoller to close conn
			if err != nil {
				wr.epoller.Remove(result.Fd())
			}
		}
	}
}

func (wr *WSRing) Read(result iouring.Result) error {

	buffer, _ := result.GetRequestBuffer()
	num := result.ReturnValue0().(int)
	content := buffer[:num]

	buffer = buffer[:0]
	wr.memPool <- &buffer

	connFD := result.Fd()
	r := bytes.NewReader(content)
	data, opCode, err := readData(r, ws.StateServerSide, ws.OpText)

	if err != nil {
		return err
	}

	if len(wr.epoller.connections) < 100 {
		logrus.WithFields(logrus.Fields{
			"id":      connFD,
			"opCode":  opCode,
			"err":     err,
			"content": string(data),
		}).Info("Read Result")
	}
	return nil
}

func readData(rw io.Reader, s ws.State, want ws.OpCode) ([]byte, ws.OpCode, error) {
	rd := wsutil.Reader{
		Source:          rw,
		State:           s,
		CheckUTF8:       true,
		SkipHeaderCheck: false,
		OnIntermediate:  nil,
	}
	for {
		hdr, err := rd.NextFrame()
		if err != nil {
			return nil, 0, err
		}
		if hdr.OpCode.IsControl() {
			continue
		}
		if hdr.OpCode&want == 0 {
			if err := rd.Discard(); err != nil {
				return nil, 0, err
			}
			continue
		}

		bts, err := ioutil.ReadAll(&rd)

		return bts, hdr.OpCode, err
	}
}
