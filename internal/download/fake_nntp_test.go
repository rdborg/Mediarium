package download

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeNNTPServer is a minimal in-process NNTP server for tests, so nothing
// here needs a live Usenet provider (CLAUDE.md: local fixtures, not live
// network calls).
type fakeNNTPServer struct {
	listener net.Listener
	bodies   map[string][]byte // message-id (without <>) -> raw article body to serve
	addr     string
	port     int
	conns    atomic.Int32 // connections accepted so far
}

func newFakeNNTPServer(t *testing.T, bodies map[string][]byte) *fakeNNTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	s := &fakeNNTPServer{listener: ln, bodies: bodies, addr: "127.0.0.1", port: port}
	go s.serve(t)
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeNNTPServer) serve(t *testing.T) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.conns.Add(1)
		go s.handle(conn)
	}
}

func (s *fakeNNTPServer) handle(conn net.Conn) {
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	rw.WriteString("200 fake nntp server ready\r\n")
	rw.Flush()

	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "AUTHINFO USER"):
			rw.WriteString("381 password required\r\n")
		case strings.HasPrefix(upper, "AUTHINFO PASS"):
			rw.WriteString("281 authentication accepted\r\n")
		case strings.HasPrefix(upper, "GROUP"):
			rw.WriteString("211 0 0 0 group selected\r\n")
		case strings.HasPrefix(upper, "BODY"):
			msgID := strings.Trim(strings.TrimSpace(line[len("BODY"):]), "<>")
			body, ok := s.bodies[msgID]
			if !ok {
				rw.WriteString("430 no such article\r\n")
				break
			}
			rw.WriteString("222 body follows\r\n")
			writeDotTerminated(rw, body)
		case upper == "QUIT":
			rw.WriteString("205 bye\r\n")
			rw.Flush()
			return
		default:
			rw.WriteString("500 command not recognized\r\n")
		}
		rw.Flush()
	}
}

// writeDotTerminated writes body as an NNTP multi-line block: dot-stuffing
// any line that starts with '.', then a lone "." terminator line.
func writeDotTerminated(rw *bufio.ReadWriter, body []byte) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, ".") {
			rw.WriteString(".")
		}
		rw.WriteString(line)
		rw.WriteString("\r\n")
	}
	rw.WriteString(".\r\n")
}
