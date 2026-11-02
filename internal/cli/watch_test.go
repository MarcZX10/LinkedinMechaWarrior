package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func badgesReply(messages int) reply {
	return reply{status: 200, body: fmt.Sprintf(`{"data":{"elements":[{"count":0,"badgingItem":"NOTIFICATIONS"},{"count":%d,"badgingItem":"MESSAGING"}]},"included":[]}`, messages)}
}

// told returns the titles the --on-change-like command channel received.
func (h *harness) told() []string {
	var titles []string
	for _, hook := range h.hooks {
		for _, v := range hook[1:] {
			if title, ok := strings.CutPrefix(v, "LMW_TITLE="); ok {
				titles = append(titles, title)
			}
		}
	}
	return titles
}

func TestWatchesNotifyWhatComesNext(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.mustRun("", "notify", "add", "phone", "command:record")
	if out := h.mustRun("", "notify", "list"); !strings.Contains(out, "phone") {
		t.Fatalf("list %s", out)
	}
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(1)
	out := h.mustRun("", "watch", "add", "unread", "--every", "15m", "--to", "phone", "--note", "tell me about messages")
	if !strings.Contains(out, "every 15m0s") {
		t.Errorf("add said %s", out)
	}
	if out := h.mustRun("", "watch", "run"); strings.Contains(out, "unread") {
		t.Errorf("a watch just checked ran again: %s", out)
	}
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(3)
	out = h.mustRun("", "watch", "run", "--all")
	if !strings.Contains(out, "3 unread messages") {
		t.Errorf("run printed %s", out)
	}
	if told := h.told(); len(told) != 1 || told[0] != "3 unread messages" {
		t.Errorf("told %q", told)
	}
	list := h.mustRun("", "watch", "list")
	if !strings.Contains(list, "tell me about messages") || !strings.Contains(list, "1 found") || !strings.Contains(list, "4 requests an hour of 6") {
		t.Errorf("list %s", list)
	}
	id := strings.Fields(list)[0]
	h.mustRun("", "watch", "remove", id)
	if list := h.mustRun("", "watch", "list"); !strings.Contains(list, "No watches") {
		t.Errorf("after remove %s", list)
	}
}

func TestOnceWatchesEndAfterTheirNotification(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.mustRun("", "notify", "add", "phone", "command:record")
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(0)
	h.mustRun("", "watch", "add", "unread", "--once")
	h.mustRun("", "watch", "run", "--all")
	if list := h.mustRun("", "watch", "list"); strings.Contains(list, "No watches") {
		t.Fatal("ended before finding anything")
	}
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(1)
	h.mustRun("", "watch", "run", "--all")
	if list := h.mustRun("", "watch", "list"); !strings.Contains(list, "No watches") {
		t.Errorf("still watching: %s", list)
	}
}

func TestWatchesKeepToTheirBudget(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(0)
	// 6.25 requests an hour allowed: half of 300 a day, over 24 hours.
	h.mustRun("", "watch", "add", "unread", "--every", "15m") // 4 an hour
	h.mustRun("", "watch", "add", "unread", "--every", "30m") // 2 more
	before := len(h.linkedin.requests)
	code, _, errOut := h.run("", "watch", "add", "unread", "--every", "1h")
	if code == 0 || !strings.Contains(errOut, "7 requests an hour, more than the 6") {
		t.Errorf("exit %d, %s", code, errOut)
	}
	if len(h.linkedin.requests) != before {
		t.Error("a refused watch still read LinkedIn")
	}
	for _, args := range [][]string{{"watch", "add", "unread", "--every", "5m"}, {"watch", "add", "tweets"}, {"watch", "add", "unread", "--to", "nowhere"},
		{"watch", "add", "unread", "--location", "Spain"}, {"notify", "add", "x", "email:me@example.com"}} {
		if code, _, _ := h.run("", args...); code == 0 {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestDaemonChecksUntilStopped(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.mustRun("", "notify", "add", "phone", "command:record")
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(0)
	h.mustRun("", "watch", "add", "unread", "--every", "15m")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.ctx = ctx
	var pauses []time.Duration
	h.wait = func(d time.Duration) {
		pauses = append(pauses, d)
		// Make the watch due and give it something new.
		h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(2)
		h.ctx = nil
		h.mustRun("", "watch", "run", "--all", "--no-notify")
		h.ctx = ctx
		if len(pauses) == 2 {
			cancel()
		}
	}
	code, out, errOut := h.run("", "daemon")
	if code != 0 || !strings.Contains(out, "lmw daemon: 1 watches") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if len(pauses) != 2 || pauses[0] < time.Minute || pauses[0] > 5*time.Minute {
		t.Errorf("pauses %v", pauses)
	}
}

func TestAssistantsCreateWatchesWithoutWriteAccess(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.mustRun("", "notify", "add", "phone", "command:record")
	h.linkedin.replies["/voyagerNotificationsDashBadgingItemCounts"] = badgesReply(0)
	answers := h.mcpSession(nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_watch","arguments":{"kind":"unread","every_minutes":20,"note":"inbox"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_watches","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"send_notification","arguments":{"text":"Your weekly summary","title":"lmw"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"check_watches","arguments":{"all":true}}}`,
	)
	names := map[string]bool{}
	for _, tool := range answers[2]["result"].(map[string]any)["tools"].([]any) {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"create_watch", "list_watches", "delete_watch", "check_watches", "list_notification_channels", "send_notification"} {
		if !names[want] {
			t.Errorf("%s not offered without --allow-write", want)
		}
	}
	if names["publish_post"] {
		t.Error("LinkedIn writes offered without --allow-write")
	}
	if text, isErr := mcpText(t, answers[3]); isErr || !strings.Contains(text, `"every_minutes": 20`) {
		t.Errorf("create_watch %s", text)
	}
	if text, _ := mcpText(t, answers[4]); !strings.Contains(text, `"note": "inbox"`) {
		t.Errorf("list_watches %s", text)
	}
	if text, isErr := mcpText(t, answers[5]); isErr || !strings.Contains(text, "phone") {
		t.Errorf("send_notification %s", text)
	}
	if text, isErr := mcpText(t, answers[6]); isErr || !strings.Contains(text, `"checked"`) {
		t.Errorf("check_watches %s", text)
	}
	if told := h.told(); len(told) != 1 || told[0] != "lmw" {
		t.Errorf("told %q", told)
	}
}
