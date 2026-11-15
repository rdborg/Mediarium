package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	timerate "golang.org/x/time/rate"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/settings"
)

// The MCP server: lets an AI assistant (a chat program, Home Assistant's voice
// assistant, a script) ask about the library and, with a clear "yes" from the
// person, change a few things. It speaks the Model Context Protocol over plain
// HTTP ("Streamable HTTP"): one JSON-RPC 2.0 message per POST to /mcp. There is
// no session, no stream the server opens by itself and no extra program: it is
// written by hand on net/http, so it adds nothing to the build.
//
// It is off until an administrator switches it on in Settings > Other apps, and
// only an administrator's API key opens it (a read-only key gets the read
// tools). The tools live in mcp_tools.go and mcp_write.go; they call the same
// code the web app calls, so a change made here is checked exactly like one
// made on a page.

const (
	// mcpMaxBody is the largest request accepted. A tool call is a few hundred bytes.
	mcpMaxBody = 256 << 10
	// mcpMaxBatch is the most messages one request may hold.
	mcpMaxBatch = 20
	// mcpMaxText is the most text one tool answer holds; longer answers are cut.
	mcpMaxText = 200 << 10
	// mcpRatePerSecond and mcpRateBurst limit one account's requests.
	mcpRatePerSecond = 20
	mcpRateBurst     = 100
	// mcpToolTime is the longest a tool may take.
	mcpToolTime = 60 * time.Second
)

// The protocol versions we answer to, newest first. A client that asks for
// another one is told the first and decides for itself whether it can follow.
var mcpVersions = []string{"2025-03-26", "2024-11-05"}

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// mcpState is what the MCP server keeps between requests.
type mcpState struct {
	once    sync.Once
	table   *routeTable  // every signed-in route; tools call the web app's own handlers through it
	dl      mcpDownloads // releases offered to an assistant and the codes that confirm a grab (mcp_download.go)
	mu      sync.Mutex
	limiter map[int64]*timerate.Limiter
}

// mcpRoutes returns the table of signed-in routes the tools call. Routes sets
// it; a Server used without Routes builds it on first use.
func (s *Server) mcpRoutes() *routeTable {
	s.mcp.once.Do(func() {
		if s.mcp.table == nil {
			s.mcp.table = s.protectedRoutes()
		}
	})
	return s.mcp.table
}

func (s *Server) mcpEnabled() bool { return s.settingOn(settings.KeyMCPEnabled, false) }

// mcpDownloadsOn is the switch for the two tools that pick a release and start
// its download (mcp_download.go). Off until an administrator turns it on.
func (s *Server) mcpDownloadsOn() bool { return s.settingOn(settings.KeyMCPDownloads, false) }

// allowMCP is the per-account request limit.
func (s *Server) allowMCP(userID int64) bool {
	s.mcp.mu.Lock()
	defer s.mcp.mu.Unlock()
	if s.mcp.limiter == nil {
		s.mcp.limiter = map[int64]*timerate.Limiter{}
	}
	l := s.mcp.limiter[userID]
	if l == nil {
		l = timerate.NewLimiter(mcpRatePerSecond, mcpRateBurst)
		s.mcp.limiter[userID] = l
	}
	return l.Allow()
}

// mcpKey is the API key a request carries: "Authorization: Bearer <key>" (what
// MCP clients send) or "X-Api-Key" (what the rest of the API takes).
func mcpKey(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("Authorization")); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-Api-Key"))
}

// mcpReply writes a plain JSON answer for the cases where there is no JSON-RPC
// message to answer (switched off, no key, too many requests).
func mcpReply(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// handleMCP is POST /mcp.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !s.mcpEnabled() {
		mcpReply(w, http.StatusNotFound, "MCP is switched off. An administrator can turn on Let AI assistants connect (MCP) in Settings > Other apps.")
		return
	}
	if r.Method == http.MethodOptions {
		oauthCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		mcpReply(w, http.StatusMethodNotAllowed, "Send MCP messages with POST.")
		return
	}
	key := mcpKey(r)
	var user *auth.User
	if key != "" {
		if auth.HasOAuthTokenPrefix(key) {
			user, _ = s.Auth.UserForOAuthToken(key)
		} else {
			user, _ = s.Auth.UserForAPIKey(key)
		}
	}
	if user == nil {
		challenge := `Bearer realm="mediarium", resource_metadata="` + s.resourceMetadataURL(r) + `"`
		if key != "" {
			challenge += `, error="invalid_token"`
		}
		w.Header().Set("WWW-Authenticate", challenge)
		mcpReply(w, http.StatusUnauthorized, "Send an administrator's API key as a Bearer token, or sign in with OAuth.")
		return
	}
	if !user.IsAdmin {
		mcpReply(w, http.StatusForbidden, "Use an administrator's API key. Accounts with limited permissions can't use MCP.")
		return
	}
	if !s.allowMCP(user.ID) {
		w.Header().Set("Retry-After", "1")
		mcpReply(w, http.StatusTooManyRequests, "Too many requests. Wait a moment and try again.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpMaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcInvalidRequest, Message: "The request is too large."}})
			return
		}
		writeJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: "The request could not be read."}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), mcpToolTime)
	defer cancel()
	c := &mcpCtx{s: s, ctx: ctx, user: user, remote: r.RemoteAddr}

	trimmed := bytes.TrimSpace(body)
	var out any // what to send; nil means the message needs no answer
	switch {
	case len(trimmed) == 0 || !json.Valid(trimmed):
		out = rpcFail(nil, rpcParseError, "That isn't valid JSON.")
	case trimmed[0] == '[':
		var batch []json.RawMessage
		if err := json.Unmarshal(trimmed, &batch); err != nil || len(batch) == 0 {
			out = rpcFail(nil, rpcInvalidRequest, "A batch must be a list with at least one message.")
			break
		}
		if len(batch) > mcpMaxBatch {
			out = rpcFail(nil, rpcInvalidRequest, "A batch can hold at most 20 messages.")
			break
		}
		var answers []rpcResponse
		for _, one := range batch {
			if a := c.handleMessage(one); a != nil {
				answers = append(answers, *a)
			}
		}
		if len(answers) > 0 {
			out = answers
		}
	default:
		if a := c.handleMessage(trimmed); a != nil {
			out = a
		}
	}
	if out == nil {
		w.WriteHeader(http.StatusAccepted) // a notification: nothing to say back
		return
	}
	mcpWrite(w, r, out)
}

// mcpWrite sends the answer as JSON, or as one server-sent event when the
// client only says it accepts a stream.
func mcpWrite(w http.ResponseWriter, r *http.Request, out any) {
	raw, err := json.Marshal(out)
	if err != nil {
		slog.Error("mcp: could not encode an answer", "err", err)
		mcpReply(w, http.StatusInternalServerError, "Couldn't build the answer.")
		return
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(append([]byte("event: message\ndata: "), raw...), '\n', '\n'))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func rpcFail(id json.RawMessage, code int, message string) *rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

// handleMessage answers one JSON-RPC message. It returns nil for a
// notification (a message without an id), which gets no answer.
func (c *mcpCtx) handleMessage(raw json.RawMessage) (answer *rpcResponse) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("mcp: a message crashed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			answer = rpcFail(nil, rpcInternalError, "Something went wrong inside Mediarium while handling that. An administrator can find the details in the log.")
		}
	}()
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcFail(nil, rpcInvalidRequest, "That isn't a JSON-RPC message.")
	}
	notification := len(req.ID) == 0
	if !notification && (len(req.ID) > 0 && (req.ID[0] == '{' || req.ID[0] == '[')) {
		return rpcFail(nil, rpcInvalidRequest, "The id must be a string or a number.")
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		if notification {
			return nil
		}
		return rpcFail(req.ID, rpcInvalidRequest, "A message needs jsonrpc 2.0 and a method.")
	}
	result, rerr := c.dispatchMethod(req)
	if notification {
		return nil
	}
	if rerr != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (c *mcpCtx) dispatchMethod(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &rpcError{Code: rpcInvalidParams, Message: "The initialize parameters aren't valid."}
			}
		}
		version := mcpVersions[0]
		for _, v := range mcpVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"listChanged": false, "subscribe": false},
				"prompts":   map[string]any{"listChanged": false},
			},
			"serverInfo":   map[string]any{"name": "Mediarium", "version": c.s.version},
			"instructions": mcpInstructions,
		}, nil
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": c.listTools()}, nil
	case "tools/call":
		return c.callTool(req.Params)
	case "resources/list":
		return map[string]any{"resources": mcpResources}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []any{}}, nil
	case "resources/read":
		return c.readResource(req.Params)
	case "prompts/list":
		return map[string]any{"prompts": mcpPromptList()}, nil
	case "prompts/get":
		return c.getPrompt(req.Params)
	}
	return nil, &rpcError{Code: rpcMethodNotFound, Message: "Method not found: " + req.Method}
}

// mcpInstructions is what the assistant is told about the server when it connects.
const mcpInstructions = "Mediarium manages a media library: movies, TV, music and books. " +
	"Use the read tools to answer questions. The tools that change something need confirm set to true; " +
	"call them first without it, tell the person what would change, and only repeat the call with confirm true once they have said yes. " +
	"To download a specific release (when an administrator has allowed it), use find_releases, then grab_release: its first call answers with a confirm_token; " +
	"tell the person what would be downloaded, and only after they say yes call it again with that token."

// ---- Settings: the switch ----

type mcpSettingsPayload struct {
	Enabled   bool   `json:"enabled"`
	Downloads bool   `json:"downloads"` // assistants may pick a release and start its download (after a confirming second call)
	Path      string `json:"path"`      // the address, after the host (and URL base)
}

func (s *Server) mcpSettingsNow() mcpSettingsPayload {
	return mcpSettingsPayload{Enabled: s.mcpEnabled(), Downloads: s.mcpDownloadsOn(), Path: "/mcp"}
}

func (s *Server) handleGetMCP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mcpSettingsNow())
}

// handlePutMCP switches the MCP address on or off. Switching it on can only be
// done from a signed-in browser session, never with an API key: a leaked key
// must not be able to open the door it would then use.
func (s *Server) handlePutMCP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled   *bool `json:"enabled"`
		Downloads *bool `json:"downloads"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Enabled == nil && req.Downloads == nil) {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if ((req.Enabled != nil && *req.Enabled) || (req.Downloads != nil && *req.Downloads)) && !requireSession(w, r) {
		return
	}
	for _, sw := range []struct {
		name string
		key  string
		val  *bool
	}{{"mcp", settings.KeyMCPEnabled, req.Enabled}, {"mcp downloads", settings.KeyMCPDownloads, req.Downloads}} {
		if sw.val == nil {
			continue
		}
		if err := setBool(s.Settings, sw.key, *sw.val); err != nil {
			slog.Error("mcp: could not save the setting", "setting", sw.key, "err", err)
			writeError(w, http.StatusInternalServerError, "Couldn't save that. Try again.")
			return
		}
		state := "off"
		if *sw.val {
			state = "on"
		}
		slog.Warn(sw.name+": switched "+state, "by", actorName(r), "from", s.clientIP(r))
	}
	writeJSON(w, http.StatusOK, s.mcpSettingsNow())
}
