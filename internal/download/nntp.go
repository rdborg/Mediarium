package download

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// NNTPConn is a single connection to a Usenet server. The engine opens one
// per configured "connections" slot (connection count
// is part of the download client config) so segment fetches run in
// parallel.
type NNTPConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

type NNTPError struct {
	Code    int
	Message string
}

func (e *NNTPError) Error() string { return fmt.Sprintf("nntp %d: %s", e.Code, e.Message) }

// DialNNTP connects (optionally over TLS) and reads the server's greeting.
func DialNNTP(host string, port int, useSSL bool, timeout time.Duration) (*NNTPConn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}

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
	code, _, err := c.readStatusLine()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read nntp greeting: %w", err)
	}
	if code != 200 && code != 201 {
		conn.Close()
		return nil, &NNTPError{Code: code, Message: "unexpected greeting"}
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

func (c *NNTPConn) Quit() error {
	_, _, _ = c.command("QUIT")
	return c.conn.Close()
}

func (c *NNTPConn) command(cmd string) (code int, message string, err error) {
	if _, err := c.conn.Write([]byte(cmd + "\r\n")); err != nil {
		return 0, "", fmt.Errorf("write nntp command %q: %w", cmd, err)
	}
	return c.readStatusLine()
}

func (c *NNTPConn) readStatusLine() (code int, message string, err error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return 0, "", fmt.Errorf("read nntp status line: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, "", fmt.Errorf("malformed nntp status line: %q", line)
	}
	code, err = strconv.Atoi(line[:3])
	if err != nil {
		return 0, "", fmt.Errorf("malformed nntp status code in %q: %w", line, err)
	}
	message = strings.TrimSpace(line[3:])
	return code, message, nil
}

// readDotTerminated reads a multi-line NNTP block, undoing dot-stuffing
// (a leading ".." on a line means a literal "." — RFC 3977 §3.1.1) and
// stopping at the lone "." terminator line.
func (c *NNTPConn) readDotTerminated() ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := c.reader.ReadString('\n')
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
		buf.WriteString(trimmed)
		buf.WriteString("\n")
	}
}
