// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package server

import (
	"crypto/tls"
	"net"
	"strconv"
	"strings"
	"sync"
)

// DataSocket describes a data socket is used to send non-control data between the client and
// server.
type DataSocket interface {
	Host() string

	Port() int

	// the standard io.Reader interface
	Read(p []byte) (n int, err error)

	// the standard io.Writer interface
	Write(p []byte) (n int, err error)

	// the standard io.Closer interface
	Close() error
}

type ftpActiveSocket struct {
	conn   *net.TCPConn
	host   string
	port   int
	logger Logger
}

func newActiveSocket(remote string, port int, logger Logger, sessionID string) (DataSocket, error) {
	connectTo := net.JoinHostPort(remote, strconv.Itoa(port))

	logger.Print(sessionID, "Opening active data connection to "+connectTo)

	raddr, err := net.ResolveTCPAddr("tcp", connectTo)

	if err != nil {
		logger.Print(sessionID, err)
		return nil, err
	}

	tcpConn, err := net.DialTCP("tcp", nil, raddr)

	if err != nil {
		logger.Print(sessionID, err)
		return nil, err
	}

	socket := new(ftpActiveSocket)
	socket.conn = tcpConn
	socket.host = remote
	socket.port = port
	socket.logger = logger

	return socket, nil
}

func (socket *ftpActiveSocket) Host() string {
	return socket.host
}

func (socket *ftpActiveSocket) Port() int {
	return socket.port
}

func (socket *ftpActiveSocket) Read(p []byte) (n int, err error) {
	return socket.conn.Read(p)
}

func (socket *ftpActiveSocket) Write(p []byte) (n int, err error) {
	return socket.conn.Write(p)
}

func (socket *ftpActiveSocket) Close() error {
	return socket.conn.Close()
}

type ftpPassiveSocket struct {
	conn       net.Conn
	port       int
	host       string
	ingress    chan []byte
	egress     chan []byte
	logger     Logger
	lock       sync.Mutex
	err        error
	tlsConfing *tls.Config
}

func newPassiveSocket(host string, port int, logger Logger, sessionID string, tlsConfing *tls.Config) (DataSocket, error) {
	socket := new(ftpPassiveSocket)
	socket.ingress = make(chan []byte)
	socket.egress = make(chan []byte)
	socket.logger = logger
	socket.host = host
	socket.port = port
	if err := socket.GoListenAndServe(sessionID); err != nil {
		return nil, err
	}
	return socket, nil
}

func (socket *ftpPassiveSocket) Host() string {
	return socket.host
}

func (socket *ftpPassiveSocket) Port() int {
	return socket.port
}

func (socket *ftpPassiveSocket) Read(p []byte) (n int, err error) {
	if err := socket.waitForOpenSocket(); err != nil {
		return 0, err
	}
	return socket.conn.Read(p)
}

func (socket *ftpPassiveSocket) Write(p []byte) (n int, err error) {
	if err := socket.waitForOpenSocket(); err != nil {
		return 0, err
	}
	return socket.conn.Write(p)
}

func (socket *ftpPassiveSocket) Close() error {
	if socket.conn != nil {
		return socket.conn.Close()
	}
	return nil
}

func (socket *ftpPassiveSocket) GoListenAndServe(sessionID string) (err error) {
	laddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("", strconv.Itoa(socket.port)))
	if err != nil {
		socket.logger.Print(sessionID, err)
		return
	}

	var listener net.Listener
	listener, err = net.ListenTCP("tcp", laddr)
	if err != nil {
		socket.logger.Print(sessionID, err)
		return
	}

	add := listener.Addr()
	parts := strings.Split(add.String(), ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		socket.logger.Print(sessionID, err)
		return
	}

	socket.port = port
	if socket.tlsConfing != nil {
		listener = tls.NewListener(listener, socket.tlsConfing)
	}

	go func() {
		socket.lock.Lock()
		defer socket.lock.Unlock()

		conn, err := listener.Accept()
		if err != nil {
			socket.err = err
			return
		}
		socket.err = nil
		socket.conn = conn
	}()
	return nil
}

func (socket *ftpPassiveSocket) waitForOpenSocket() error {
	socket.lock.Lock()
	defer socket.lock.Unlock()
	if socket.conn != nil {
		return nil
	}
	return socket.err
}
