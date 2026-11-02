package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type received struct {
	path, contentType, title, click string
	body                            string
}

func listen(t *testing.T) (*httptest.Server, *[]received) {
	t.Helper()
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, received{r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Title"), r.Header.Get("Click"), string(body)})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestSendToEachKindOfChannel(t *testing.T) {
	srv, got := listen(t)
	m := Message{Title: "New post by Grace Hopper", Text: "Compilers are fun", URL: "https://www.linkedin.com/feed/update/urn:li:activity:1/", Watch: "a1b2c3"}
	var ran []string
	s := Sender{TelegramAPI: srv.URL, Shell: func(_ context.Context, command string, env []string) error {
		ran = append(append(ran, command), env...)
		return nil
	}}
	for _, target := range []string{
		"ntfy:" + srv.URL + "/lmw-topic",
		"telegram:123456:ABC-def:987",
		"discord:" + srv.URL + "/discord",
		"slack:" + srv.URL + "/slack",
		"webhook:" + srv.URL + "/hook",
		"command:notify-send hi",
	} {
		if err := s.Send(context.Background(), target, m); err != nil {
			t.Errorf("%s: %v", target, err)
		}
	}
	if len(*got) != 5 {
		t.Fatalf("got %d requests", len(*got))
	}
	ntfy, telegram, discord, slack, hook := (*got)[0], (*got)[1], (*got)[2], (*got)[3], (*got)[4]
	if ntfy.path != "/lmw-topic" || ntfy.title != m.Title || ntfy.click != m.URL || ntfy.body != m.Text {
		t.Errorf("ntfy %+v", ntfy)
	}
	var tg map[string]any
	_ = json.Unmarshal([]byte(telegram.body), &tg)
	if telegram.path != "/bot123456:ABC-def/sendMessage" || tg["chat_id"] != "987" || !strings.Contains(tg["text"].(string), "Compilers are fun") {
		t.Errorf("telegram %+v", telegram)
	}
	if !strings.Contains(discord.body, `"content":"New post by Grace Hopper\nCompilers are fun\nhttps://`) {
		t.Errorf("discord %s", discord.body)
	}
	if !strings.Contains(slack.body, `"text":"New post by Grace Hopper`) {
		t.Errorf("slack %s", slack.body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(hook.body), &payload); err != nil || payload["watch"] != "a1b2c3" || payload["url"] != m.URL || payload["time"] == "" {
		t.Errorf("webhook %s", hook.body)
	}
	if strings.Join(ran, "|") != "notify-send hi|LMW_TITLE=New post by Grace Hopper|LMW_TEXT=Compilers are fun|LMW_URL="+m.URL+"|LMW_WATCH=a1b2c3" {
		t.Errorf("command %q", ran)
	}
}

func TestFailuresDoNotLeakTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	err := Sender{TelegramAPI: srv.URL}.Send(context.Background(), "telegram:123456:SECRET:987", Message{Title: "x"})
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "401") {
		t.Errorf("error %v", err)
	}
	srv.Close()
	err = Sender{TelegramAPI: srv.URL}.Send(context.Background(), "telegram:123456:SECRET:987", Message{Title: "x"})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("unreachable error %v", err)
	}
}

func TestValidateAndMask(t *testing.T) {
	for _, bad := range []string{"", "email:me@example.com", "ntfy:", "ntfy:a/b", "telegram:123", "discord:http://example.com/x", "webhook:not a url", "command: "} {
		if Validate(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"ntfy:topic", "ntfy:https://ntfy.example.com/t", "telegram:1:A:2", "slack:https://hooks.slack.com/x", "webhook:http://localhost:8080/x", "command:true"} {
		if err := Validate(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	cases := map[string]string{
		"telegram:123456:SECRET:987":                        "telegram:123456:…:987",
		"discord:https://discord.com/api/webhooks/1/SECRET": "discord:https://discord.com/…",
		"ntfy:my-secret-topic":                              "ntfy:my-…",
		"command:notify-send hi":                            "command:notify-send hi",
	}
	for target, want := range cases {
		if got := Mask(target); got != want {
			t.Errorf("Mask(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestStore(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "notify.json")}
	if channels, err := s.Load(); err != nil || channels != nil {
		t.Fatalf("empty store %v, %v", channels, err)
	}
	want := []Channel{{Name: "phone", Target: "ntfy:topic"}, {Name: "team", Target: "slack:https://hooks.slack.com/x"}}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	channels, err := s.Load()
	if err != nil || len(channels) != 2 || channels[1].Name != "team" {
		t.Fatalf("loaded %v, %v", channels, err)
	}
	if picked, err := Pick(channels, ""); err != nil || len(picked) != 2 {
		t.Errorf("all channels %v, %v", picked, err)
	}
	if picked, err := Pick(channels, "phone"); err != nil || len(picked) != 1 || picked[0].Target != "ntfy:topic" {
		t.Errorf("one channel %v, %v", picked, err)
	}
	if _, err := Pick(channels, "nope"); err == nil {
		t.Error("unknown channel accepted")
	}
}
