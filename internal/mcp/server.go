// Package mcp serves tools over the Model Context Protocol (MCP), so that an
// AI assistant can use lmw.
//
// The transport is stdio: one JSON-RPC 2.0 message per line on stdin and
// stdout. Only what lmw needs is implemented: the lifecycle (initialize,
// ping), tools (list and call), cancellation, and form elicitation, which
// asks the person to accept a tool call that changes something on LinkedIn.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// ProtocolVersions are the MCP revisions this server speaks, newest first.
var ProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	maxMessageBytes = 16 << 20

	parseError     = -32700
	invalidRequest = -32600
	methodNotFound = -32601
	invalidParams  = -32602
)

// Server answers MCP requests with a fixed set of tools.
type Server struct {
	Name         string
	Title        string
	Version      string
	Instructions string
	Tools        []Tool
	// AllowWrite offers the tools that change something on LinkedIn.
	AllowWrite bool

	out     io.Writer
	writeMu sync.Mutex

	mu          sync.Mutex
	nextID      int
	pending     map[string]chan message
	inflight    map[string]context.CancelFunc
	elicitation bool

	inbox inbox
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type outgoing struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve handles requests from in and writes the answers to out until in ends.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = out
	s.pending = map[string]chan message{}
	s.inflight = map[string]context.CancelFunc{}
	s.inbox.ready = make(chan struct{}, 1)
	go s.read(in)
	for {
		msg, ok, err := s.inbox.next(ctx)
		if !ok {
			return err
		}
		s.handle(ctx, msg)
	}
}

// read runs in its own goroutine so that answers to the server's own requests
// (elicitation) and cancellations arrive while a tool is running.
func (s *Server) read(in io.Reader) {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			s.send(outgoing{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: parseError, Message: "invalid JSON: " + err.Error()}})
			continue
		}
		switch {
		case msg.Method == "" && len(msg.ID) > 0:
			s.deliver(msg)
		case msg.Method == "notifications/cancelled":
			s.cancel(msg.Params)
		default:
			s.inbox.push(msg)
		}
	}
	s.mu.Lock()
	for id, ch := range s.pending {
		close(ch)
		delete(s.pending, id)
	}
	s.mu.Unlock()
	s.inbox.close(scanner.Err())
}

func (s *Server) handle(ctx context.Context, msg message) {
	isRequest := len(msg.ID) > 0 && string(msg.ID) != "null"
	var result any
	var rerr *rpcError
	switch msg.Method {
	case "initialize":
		result, rerr = s.initialize(msg.Params)
	case "ping":
		result = struct{}{}
	case "tools/list":
		result = map[string]any{"tools": s.listTools()}
	case "tools/call":
		result, rerr = s.callTool(ctx, msg.ID, msg.Params)
	case "":
		rerr = &rpcError{Code: invalidRequest, Message: "missing method"}
	default:
		if strings.HasPrefix(msg.Method, "notifications/") {
			return
		}
		rerr = &rpcError{Code: methodNotFound, Message: "method not found: " + msg.Method}
	}
	if !isRequest {
		return
	}
	if rerr != nil {
		result = nil
	}
	s.send(outgoing{JSONRPC: "2.0", ID: msg.ID, Result: result, Error: rerr})
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Elicitation map[string]any `json:"elicitation"`
		} `json:"capabilities"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: invalidParams, Message: "invalid initialize parameters: " + err.Error()}
		}
	}
	version := ProtocolVersions[0]
	for _, v := range ProtocolVersions {
		if v == p.ProtocolVersion {
			version = v
		}
	}
	// An empty elicitation capability means form mode, as in older revisions.
	elicit := p.Capabilities.Elicitation
	s.mu.Lock()
	s.elicitation = elicit != nil && (len(elicit) == 0 || elicit["form"] != nil)
	s.mu.Unlock()

	info := map[string]any{"name": s.Name, "version": s.Version}
	if s.Title != "" {
		info["title"] = s.Title
	}
	result := map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      info,
	}
	if s.Instructions != "" {
		result["instructions"] = s.Instructions
	}
	return result, nil
}

func (s *Server) offered(t Tool) bool { return t.ReadOnly || s.AllowWrite }

func (s *Server) listTools() []any {
	tools := []any{}
	for _, t := range s.Tools {
		if s.offered(t) {
			tools = append(tools, t.describe())
		}
	}
	return tools
}

func (s *Server) callTool(ctx context.Context, id json.RawMessage, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: invalidParams, Message: "invalid tools/call parameters: " + err.Error()}
	}
	var tool *Tool
	for i := range s.Tools {
		if s.Tools[i].Name == p.Name {
			tool = &s.Tools[i]
		}
	}
	if tool == nil {
		return nil, &rpcError{Code: invalidParams, Message: "unknown tool: " + p.Name}
	}
	if !s.offered(*tool) {
		return toolError(fmt.Errorf("%s changes something on LinkedIn, and this server was started without permission to write. "+
			"The person can allow it by starting it with `lmw mcp --allow-write`", tool.Name)), nil
	}
	args, err := tool.check(p.Arguments)
	if err != nil {
		return toolError(err), nil
	}

	ctx, cancel := context.WithCancel(ctx)
	key := idKey(id)
	s.mu.Lock()
	s.inflight[key] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		cancel()
	}()

	if !tool.ReadOnly && tool.Confirm != nil && s.canElicit() {
		question, err := tool.Confirm(args)
		if err != nil {
			return toolError(err), nil
		}
		accepted, err := s.confirm(ctx, question)
		if err != nil {
			return toolError(fmt.Errorf("could not ask the person to confirm: %w", err)), nil
		}
		if !accepted {
			return toolError(errors.New("the person did not accept; nothing was changed on LinkedIn")), nil
		}
	}

	value, err := tool.Run(ctx, args)
	if err != nil {
		return toolError(err), nil
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return toolError(err), nil
	}
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(raw)}},
		"isError": false,
	}, nil
}

func toolError(err error) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": err.Error()}},
		"isError": true,
	}
}

func (s *Server) canElicit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.elicitation
}

// confirm asks the person, through the client, to accept an action.
func (s *Server) confirm(ctx context.Context, question string) (bool, error) {
	var answer struct {
		Action string `json:"action"`
	}
	err := s.request(ctx, "elicitation/create", map[string]any{
		"message":         question,
		"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	}, &answer)
	return answer.Action == "accept", err
}

// request sends a request to the client and waits for its answer.
func (s *Server) request(ctx context.Context, method string, params, result any) error {
	s.mu.Lock()
	s.nextID++
	id := json.RawMessage(fmt.Sprintf(`"lmw-%d"`, s.nextID))
	answer := make(chan message, 1)
	s.pending[idKey(id)] = answer
	s.mu.Unlock()

	s.send(outgoing{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	select {
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, idKey(id))
		s.mu.Unlock()
		return ctx.Err()
	case msg, ok := <-answer:
		if !ok {
			return errors.New("the client disconnected")
		}
		if msg.Error != nil {
			return fmt.Errorf("%s (code %d)", msg.Error.Message, msg.Error.Code)
		}
		return json.Unmarshal(msg.Result, result)
	}
}

func (s *Server) deliver(msg message) {
	key := idKey(msg.ID)
	s.mu.Lock()
	answer, ok := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if ok {
		answer <- msg
	}
}

func (s *Server) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	s.mu.Lock()
	cancel, ok := s.inflight[idKey(p.RequestID)]
	s.mu.Unlock()
	if ok {
		cancel()
	}
}

func (s *Server) send(msg outgoing) {
	raw, err := json.Marshal(msg)
	if err != nil {
		slog.Error("cannot encode an MCP message", "error", err)
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.out.Write(append(raw, '\n')); err != nil {
		slog.Debug("cannot write an MCP message", "error", err)
	}
}

// idKey compares JSON-RPC ids by value, so 1, 1.0 and "1" stay distinct but
// formatting differences don't matter.
func idKey(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return fmt.Sprintf("%T:%v", v, v)
}

// inbox queues incoming requests without ever blocking the reader.
type inbox struct {
	mu    sync.Mutex
	items []message
	done  bool
	err   error
	ready chan struct{}
}

func (b *inbox) push(msg message) {
	b.mu.Lock()
	b.items = append(b.items, msg)
	b.mu.Unlock()
	b.notify()
}

func (b *inbox) close(err error) {
	b.mu.Lock()
	b.done, b.err = true, err
	b.mu.Unlock()
	b.notify()
}

func (b *inbox) notify() {
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

// next returns the next message, or ok=false once the input has ended.
func (b *inbox) next(ctx context.Context) (message, bool, error) {
	for {
		b.mu.Lock()
		if len(b.items) > 0 {
			msg := b.items[0]
			b.items = b.items[1:]
			b.mu.Unlock()
			return msg, true, nil
		}
		done, err := b.done, b.err
		b.mu.Unlock()
		if done {
			return message{}, false, err
		}
		select {
		case <-b.ready:
		case <-ctx.Done():
			return message{}, false, ctx.Err()
		}
	}
}
