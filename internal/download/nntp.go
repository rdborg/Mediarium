package download

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

// NNTPConn is a single connection to a Usenet server. The engine opens one
// per configured "connections" slot (connection count
// is part of the download client config) so segment fetches run in
// parallel.
type NNTPConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

// nntpIOTimeout is how long a server may stay silent before a command or a
// line of an article is given up on. Without it a server that stops answering
// (or a half-open connection after a network change) would hold a download
// and its connection for ever.
var nntpIOTimeout = 60 * time.Second

type NNTPError struct {
	Code    int
	Message string
}

func (e *NNTPError) Error() string { return fmt.Sprintf("nntp %d: %s", e.Code, e.Message) }

// DialNNTP connects (optionally over TLS) and reads the server's greeting.
func DialNNTP(host string, port int, useSSL bool, timeout time.Duration) (*NNTPConn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := netguard.Dialer(timeout)

	var conn net.Conn
	var err error
	if useSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial nntp %s: %w", addr, err)
	}

	c := &NNTPConn{conn: conn, reader: bufio.NewReader(conn)}
	conn.SetDeadline(time.Now().Add(nntpIOTimeout)) //nolint:errcheck
	code, msg, err := c.readStatusLine()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read nntp greeting: %w", err)
	}
	if code != 200 && code != 201 {
		conn.Close()
		// A server that is full or refuses you says so in its greeting.
		if msg == "" {
			msg = "unexpected greeting"
		}
		return nil, &NNTPError{Code: code, Message: msg}
	}
	return c, nil
}

// Authenticate performs AUTHINFO USER/PASS. Skipped entirely if username
// is empty (some providers/block accounts don't require it).
func (c *NNTPConn) Authenticate(username, password string) error {
	if username == "" {
		return nil
	}
	code, msg, err := c.command("AUTHINFO USER " + username)
	if err != nil {
		return err
	}
	if code == 281 {
		return nil // accepted without needing a password
	}
	if code != 381 {
		return &NNTPError{Code: code, Message: msg}
	}
	code, msg, err = c.command("AUTHINFO PASS " + password)
	if err != nil {
		return err
	}
	if code != 281 {
		return &NNTPError{Code: code, Message: msg}
	}
	return nil
}

// SelectGroup issues GROUP <name>. Several providers require at least one
// successful GROUP before BODY-by-message-id works in that session.
func (c *NNTPConn) SelectGroup(name string) error {
	code, msg, err := c.command("GROUP " + name)
	if err != nil {
		return err
	}
	if code != 211 {
		return &NNTPError{Code: code, Message: msg}
	}
	return nil
}

// FetchBody retrieves one article's raw body (still yEnc-encoded) by
// message-id, e.g. "<abc123@example>".
func (c *NNTPConn) FetchBody(messageID string) ([]byte, error) {
	if !strings.HasPrefix(messageID, "<") {
		messageID = "<" + messageID + ">"
	}
	code, msg, err := c.command("BODY " + messageID)
	if err != nil {
		return nil, err
	}
	if code != 222 {
		return nil, &NNTPError{Code: code, Message: msg}
	}
	return c.readDotTerminated()
}

// Quit says goodbye and closes the connection. It never waits long for the
// server's answer, and always closes.
func (c *NNTPConn) Quit() error {
	c.conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	_, _ = c.conn.Write([]byte("QUIT\r\n"))
	return c.conn.Close()
}

// Close closes the connection at once, without saying goodbye. It also wakes
// up anything blocked reading from it.
func (c *NNTPConn) Close() error { return c.conn.Close() }

func (c *NNTPConn) command(cmd string) (code int, message string, err error) {
	// One command per line. A line break (or a NUL) inside a value, such as a
	// message-id taken from a hostile NZB, would be read by the server as
	// further commands sent with this login.
	if strings.ContainsAny(cmd, "\r\n\x00") {
		return 0, "", errors.New("refusing to send an NNTP command that contains a line break")
	}
	c.conn.SetDeadline(time.Now().Add(nntpIOTimeout)) //nolint:errcheck
	if _, err := c.conn.Write([]byte(cmd + "\r\n")); err != nil {
		// only the command word: the rest can be a user name or a password
		verb, _, _ := strings.Cut(cmd, " ")
		return 0, "", fmt.Errorf("write nntp command %s: %w", verb, err)
	}
	return c.readStatusLine()
}

func (c *NNTPConn) readStatusLine() (code int, message string, err error) {
	line, err := readBoundedLine(c.reader, maxNNTPStatusLine)
	if err != nil {
		return 0, "", fmt.Errorf("read nntp status line: %w", err)
	}
	return parseStatusLine(line)
}

// parseStatusLine splits "211 1234 1 1234 group" into the three-digit code
// and the text after it. The code must be three plain digits: strconv would
// also take a sign ("+20"), which no server sends.
func parseStatusLine(line string) (code int, message string, err error) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, "", fmt.Errorf("malformed nntp status line: %q", line)
	}
	for i := 0; i < 3; i++ {
		if line[i] < '0' || line[i] > '9' {
			return 0, "", fmt.Errorf("malformed nntp status code in %q", line)
		}
	}
	code = int(line[0]-'0')*100 + int(line[1]-'0')*10 + int(line[2]-'0')
	message = strings.TrimSpace(line[3:])
	return code, message, nil
}

const (
	// maxNNTPStatusLine is the longest status line accepted. Real ones are a
	// few dozen bytes.
	maxNNTPStatusLine = 8 << 10
	// maxArticleLine and maxArticleBytes cap one line and the whole body of
	// an article, so a server that never sends a line break or a terminating
	// dot cannot make the download use all the memory. Articles are well
	// under 1 MB in practice.
	maxArticleLine  = 2 << 20
	maxArticleBytes = 16 << 20
)

// errLineTooLong is returned when a line from the server passes its limit.
var errLineTooLong = errors.New("the server sent a line that is too long")

// readBoundedLine reads up to and including the next newline, and gives up
// once max bytes have arrived without one.
func readBoundedLine(r *bufio.Reader, max int) (string, error) {
	var acc []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(acc) == 0 && err == nil {
			if len(chunk) > max {
				return "", errLineTooLong
			}
			return string(chunk), nil
		}
		acc = append(acc, chunk...)
		if len(acc) > max {
			return "", errLineTooLong
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", err
		}
		return string(acc), nil
	}
}

// readDotTerminated reads a multi-line NNTP block (see readDotBlock), keeping
// the connection's read deadline moving as lines arrive.
func (c *NNTPConn) readDotTerminated() ([]byte, error) {
	return readDotBlock(c.reader, func() {
		c.conn.SetReadDeadline(time.Now().Add(nntpIOTimeout)) //nolint:errcheck
	})
}

// readDotBlock reads a multi-line NNTP block, undoing dot-stuffing
// (a leading ".." on a line means a literal "." — RFC 3977 §3.1.1) and
// stopping at the lone "." terminator line. beforeLine, when set, runs before
// each line is read.
func readDotBlock(r *bufio.Reader, beforeLine func()) ([]byte, error) {
	var buf bytes.Buffer
	for {
		if beforeLine != nil {
			beforeLine()
		}
		line, err := readBoundedLine(r, maxArticleLine)
		if err != nil {
			return nil, fmt.Errorf("read nntp multiline block: %w", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			return buf.Bytes(), nil
		}
		if strings.HasPrefix(trimmed, "..") {
			trimmed = trimmed[1:]
		}
		if buf.Len()+len(trimmed)+1 > maxArticleBytes {
			return nil, fmt.Errorf("read nntp multiline block: %w", errLineTooLong)
		}
		buf.WriteString(trimmed)
		buf.WriteString("\n")
	}
}

// TooManyConnectionsMessage is what a person is told when the provider says
// the login has too many connections.
const TooManyConnectionsMessage = "Your Usenet provider says this login has too many connections open. Stop other programs using the account, or lower the connections here."

// IsTooManyConnections reports whether err is the provider saying the login
// is using more connections than its plan allows: "502 Too many connections",
// or the same in words on any code. A plain 502 also counts unless the server
// is clearly talking about the login itself.
func IsTooManyConnections(err error) bool {
	var ne *NNTPError
	if !errors.As(err, &ne) {
		return false
	}
	msg := strings.ToLower(ne.Message)
	switch {
	case strings.Contains(msg, "too many"),
		strings.Contains(msg, "connection limit"),
		strings.Contains(msg, "max connections"),
		strings.Contains(msg, "maximum number of connections"),
		strings.Contains(msg, "exceeded") && strings.Contains(msg, "connection"):
		return true
	}
	if ne.Code != 502 {
		return false
	}
	return !strings.Contains(msg, "auth") && !strings.Contains(msg, "password") && !strings.Contains(msg, "credential") && !strings.Contains(msg, "denied")
}

// FriendlyError says what went wrong with a news server in plain words, for
// the connection test and the download queue. ok is false when there is
// nothing better to say than the error itself.
func FriendlyError(err error) (message string, ok bool) {
	if err == nil {
		return "", false
	}
	if IsTooManyConnections(err) {
		return TooManyConnectionsMessage, true
	}
	var ne *NNTPError
	if errors.As(err, &ne) && (ne.Code == 481 || ne.Code == 482 || ne.Code == 502) {
		return "The provider refused this username and password.", true
	}
	return "", false
}
