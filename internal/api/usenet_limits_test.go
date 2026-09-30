package api_test

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/download"
)

// refusingNNTP answers like a full provider: a greeting, then "502 Too many
// connections." when the login is offered (or in the greeting itself).
func refusingNNTP(t *testing.T, inGreeting bool, reply string) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
				if inGreeting {
					rw.WriteString(reply + "\r\n")
					rw.Flush()
					return
				}
				rw.WriteString("200 hello\r\n")
				rw.Flush()
				for {
					line, err := rw.ReadString('\n')
					if err != nil {
						return
					}
					switch upper := strings.ToUpper(strings.TrimSpace(line)); {
					case strings.HasPrefix(upper, "AUTHINFO USER"):
						rw.WriteString("381 password?\r\n")
					case strings.HasPrefix(upper, "AUTHINFO PASS"):
						rw.WriteString(reply + "\r\n")
					case upper == "QUIT":
						rw.WriteString("205 bye\r\n")
						rw.Flush()
						return
					default:
						rw.WriteString("500 what?\r\n")
					}
					rw.Flush()
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func TestConnectionTestSaysPlainlyWhenTheProviderHasTooManyConnections(t *testing.T) {
	_, base, client := newServerWithDB(t)
	tests := []struct {
		name       string
		inGreeting bool
		reply      string
		want       string
	}{
		{"502 when signing in", false, "502 Too many connections.", download.TooManyConnectionsMessage},
		{"502 in the greeting", true, "502 Too many connections.", download.TooManyConnectionsMessage},
		{"the words on a different code", false, "400 too many connections for this account", download.TooManyConnectionsMessage},
		{"wrong password", false, "481 Authentication failed", "Connected, but the login failed. The provider refused this username and password."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, port := refusingNNTP(t, tc.inGreeting, tc.reply)
			res := postJSON[map[string]any](t, client, base+"/api/usenet-servers/test", map[string]any{
				"host": host, "port": port, "useSsl": false, "username": "ryan", "password": "x",
			}, http.StatusOK)
			if res["ok"] != false || res["message"] != tc.want {
				t.Fatalf("test result = %+v, want the message %q", res, tc.want)
			}
		})
	}
}

// A new server starts with 10 connections, and the shared connection limit
// follows the server as it is added, edited and removed.
func TestServerChangesReachTheDownloaderAtOnce(t *testing.T) {
	_, base, client := newServerWithDB(t)
	host, port := refusingNNTP(t, false, "502 Too many connections.")
	key := host + ":" + strconv.Itoa(port)
	state := func() (download.ServerState, bool) {
		for _, st := range download.ServerStates() {
			if st.Host == key {
				return st, true
			}
		}
		return download.ServerState{}, false
	}

	created := postJSON[map[string]any](t, client, base+"/api/usenet-servers", map[string]any{
		"name": "Provider", "host": host, "port": port, "useSsl": false, "username": "ryan", "password": "x", "enabled": true,
	}, http.StatusCreated)
	if created["connections"] != float64(10) {
		t.Fatalf("a new server got %v connections, want 10", created["connections"])
	}
	id := fmt.Sprintf("%.0f", created["id"])
	if st, ok := state(); !ok || st.Configured != 10 || st.Limit != 10 {
		t.Fatalf("the downloader does not know the new server: %+v %v", st, ok)
	}

	// Editing the number of connections lowers the limit for downloads that are running.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/usenet-servers/"+id, map[string]any{"connections": 3, "enabled": true}, http.StatusOK)
	if st, _ := state(); st.Configured != 3 || st.Limit != 3 {
		t.Fatalf("after editing to 3 connections the downloader has %+v", st)
	}

	// Switching it off lets it go.
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/usenet-servers/"+id, map[string]any{"enabled": false}, http.StatusOK)
	if _, ok := state(); ok {
		t.Fatal("a server that was switched off is still in use by the downloader")
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/usenet-servers/"+id, map[string]any{"enabled": true}, http.StatusOK)
	if _, ok := state(); !ok {
		t.Fatal("a server that was switched back on is not known to the downloader")
	}

	// Removing it lets it go too.
	if status, _ := doStatus(t, client, http.MethodDelete, base+"/api/usenet-servers/"+id); status != http.StatusOK {
		t.Fatalf("delete: status %d", status)
	}
	if _, ok := state(); ok {
		t.Fatal("a removed server is still in use by the downloader")
	}
}
