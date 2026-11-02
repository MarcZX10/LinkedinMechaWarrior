package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// session runs a server on pipes and talks to it like a client.
type session struct {
	t      *testing.T
	in     *io.PipeWriter
	out    *bufio.Scanner
	done   chan error
	cancel context.CancelFunc
}

func start(t *testing.T, s *Server) *session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, inR, outW)
		outW.Close()
	}()
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	c := &session{t: t, in: inW, out: scanner, done: done, cancel: cancel}
	t.Cleanup(func() {
		inW.Close()
		cancel()
	})
	return c
}

func (c *session) send(msg string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, msg+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *session) receive() map[string]any {
	c.t.Helper()
	got := make(chan map[string]any, 1)
	go func() {
		if !c.out.Scan() {
			got <- nil
			return
		}
		var msg map[string]any
		if err := json.Unmarshal(c.out.Bytes(), &msg); err != nil {
			c.t.Errorf("server wrote invalid JSON %q", c.out.Text())
		}
		got <- msg
	}()
	select {
	case msg := <-got:
		if msg == nil {
			c.t.Fatal("the server closed its output")
		}
		return msg
	case <-time.After(5 * time.Second):
		c.t.Fatal("no answer from the server")
		return nil
	}
}

func (c *session) call(id int, method, params string) map[string]any {
	c.t.Helper()
	c.send(`{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"` + method + `","params":` + params + `}`)
	msg := c.receive()
	if msg["id"] != float64(id) {
		c.t.Fatalf("answer to %s has id %v, want %d: %v", method, msg["id"], id, msg)
	}
	return msg
}

func itoa(n int) string { raw, _ := json.Marshal(n); return string(raw) }

func toolText(t *testing.T, msg map[string]any) (string, bool) {
	t.Helper()
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", msg)
	}
	content := result["content"].([]any)[0].(map[string]any)
	return content["text"].(string), result["isError"] == true
}

func testServer(allowWrite bool, ran *[]string) *Server {
	return &Server{
		Name: "lmw", Version: "test", Instructions: "Use carefully.", AllowWrite: allowWrite,
		Tools: []Tool{
			{
				Name: "feed", Title: "Feed", Description: "Read the feed", ReadOnly: true,
				Params: []Param{
					{Name: "limit", Type: "integer", Description: "posts", Default: 10, Min: 1, Max: 50},
					{Name: "full", Type: "boolean", Description: "full text"},
				},
				Run: func(_ context.Context, a Args) (any, error) {
					*ran = append(*ran, "feed")
					return map[string]any{"limit": a.Int("limit"), "full": a.Bool("full")}, nil
				},
			},
			{
				Name: "react", Title: "React", Description: "React to a post",
				Params: []Param{
					{Name: "post", Type: "string", Description: "post", Required: true},
					{Name: "type", Type: "string", Description: "reaction", Enum: []string{"LIKE", "PRAISE"}, Default: "LIKE"},
				},
				Confirm: func(a Args) (string, error) {
					return "React " + a.String("type") + " to " + a.String("post") + "?", nil
				},
				Run: func(_ context.Context, a Args) (any, error) {
					*ran = append(*ran, "react "+a.String("type"))
					return map[string]any{"ok": true}, nil
				},
			},
			{
				Name: "fail", Title: "Fail", Description: "Always fails", ReadOnly: true,
				Run: func(context.Context, Args) (any, error) { return nil, errors.New("LinkedIn said no") },
			},
		},
	}
}

func TestInitializeNegotiatesTheProtocolVersion(t *testing.T) {
	var ran []string
	c := start(t, testServer(false, &ran))
	msg := c.call(1, "initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`)
	result := msg["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" || result["instructions"] != "Use carefully." {
		t.Errorf("initialize result %v", result)
	}
	if result["serverInfo"].(map[string]any)["name"] != "lmw" {
		t.Errorf("server info %v", result["serverInfo"])
	}
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	msg = c.call(2, "initialize", `{"protocolVersion":"1999-01-01","capabilities":{}}`)
	if v := msg["result"].(map[string]any)["protocolVersion"]; v != ProtocolVersions[0] {
		t.Errorf("unknown version answered with %v", v)
	}
	if msg := c.call(3, "ping", `{}`); msg["result"] == nil {
		t.Errorf("ping %v", msg)
	}
	if msg := c.call(4, "resources/list", `{}`); msg["error"].(map[string]any)["code"] != float64(methodNotFound) {
		t.Errorf("unknown method %v", msg)
	}
	c.send(`{not json`)
	if msg := c.receive(); msg["error"].(map[string]any)["code"] != float64(parseError) {
		t.Errorf("invalid JSON %v", msg)
	}
}

func TestWriteToolsNeedPermission(t *testing.T) {
	var ran []string
	c := start(t, testServer(false, &ran))
	c.call(1, "initialize", `{"protocolVersion":"2025-11-25","capabilities":{}}`)
	tools := c.call(2, "tools/list", `{}`)["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != "feed" {
		t.Fatalf("tools without --allow-write: %v", tools)
	}
	annotations := tools[0].(map[string]any)["annotations"].(map[string]any)
	if annotations["readOnlyHint"] != true {
		t.Errorf("annotations %v", annotations)
	}
	text, isError := toolText(t, c.call(3, "tools/call", `{"name":"react","arguments":{"post":"urn:li:activity:1"}}`))
	if !isError || !strings.Contains(text, "--allow-write") || len(ran) != 0 {
		t.Errorf("write without permission: %q, ran %v", text, ran)
	}

	c = start(t, testServer(true, &ran))
	c.call(1, "initialize", `{"protocolVersion":"2025-11-25","capabilities":{}}`)
	if tools := c.call(2, "tools/list", `{}`)["result"].(map[string]any)["tools"].([]any); len(tools) != 3 {
		t.Errorf("tools with --allow-write: %v", tools)
	}
	// Without elicitation, the client's own approval of the call is the confirmation.
	if _, isError := toolText(t, c.call(3, "tools/call", `{"name":"react","arguments":{"post":"urn:li:activity:1"}}`)); isError || ran[0] != "react LIKE" {
		t.Errorf("ran %v", ran)
	}
}

func TestToolArgumentsAreCheckedAndDefaulted(t *testing.T) {
	var ran []string
	c := start(t, testServer(false, &ran))
	c.call(1, "initialize", `{"protocolVersion":"2025-11-25","capabilities":{}}`)

	text, isError := toolText(t, c.call(2, "tools/call", `{"name":"feed","arguments":{}}`))
	if isError || !strings.Contains(text, `"limit": 10`) || !strings.Contains(text, `"full": false`) {
		t.Errorf("defaults: %q", text)
	}
	for args, want := range map[string]string{
		`{"limit":500}`:   "between 1 and 50",
		`{"limit":2.5}`:   "whole number",
		`{"full":"yes"}`:  "true or false",
		`{"colour":"no"}`: "unknown argument colour",
	} {
		text, isError := toolText(t, c.call(3, "tools/call", `{"name":"feed","arguments":`+args+`}`))
		if !isError || !strings.Contains(text, want) {
			t.Errorf("%s: %q", args, text)
		}
	}
	if text, isError := toolText(t, c.call(4, "tools/call", `{"name":"fail","arguments":{}}`)); !isError || text != "LinkedIn said no" {
		t.Errorf("failing tool: %q", text)
	}
	if msg := c.call(5, "tools/call", `{"name":"nope","arguments":{}}`); msg["error"] == nil {
		t.Errorf("unknown tool %v", msg)
	}
	if len(ran) != 1 {
		t.Errorf("tools ran %v", ran)
	}
}

func TestWriteToolsAskThePersonThroughElicitation(t *testing.T) {
	var ran []string
	c := start(t, testServer(true, &ran))
	c.call(1, "initialize", `{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}`)

	for _, tc := range []struct {
		action string
		ran    int
	}{{"decline", 0}, {"accept", 1}} {
		c.send(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"react","arguments":{"post":"urn:li:activity:1","type":"PRAISE"}}}`)
		ask := c.receive()
		params := ask["params"].(map[string]any)
		if ask["method"] != "elicitation/create" || params["message"] != "React PRAISE to urn:li:activity:1?" {
			t.Fatalf("expected an elicitation, got %v", ask)
		}
		// A ping while the person decides is answered after the tool call.
		c.send(`{"jsonrpc":"2.0","id":8,"method":"ping"}`)
		c.send(`{"jsonrpc":"2.0","id":` + mustMarshal(ask["id"]) + `,"result":{"action":"` + tc.action + `"}}`)
		answer := c.receive()
		if answer["id"] != float64(7) {
			t.Fatalf("expected the tool result, got %v", answer)
		}
		text, isError := toolText(t, answer)
		if (tc.action == "decline") != isError || len(ran) != tc.ran {
			t.Errorf("%s: %q, ran %v", tc.action, text, ran)
		}
		if pong := c.receive(); pong["id"] != float64(8) {
			t.Errorf("expected the ping answer, got %v", pong)
		}
	}
}

func TestServeEndsWithItsInput(t *testing.T) {
	var ran []string
	c := start(t, testServer(false, &ran))
	c.call(1, "initialize", `{"protocolVersion":"2025-11-25","capabilities":{}}`)
	c.in.Close()
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after its input ended")
	}
}

func mustMarshal(v any) string { raw, _ := json.Marshal(v); return string(raw) }
