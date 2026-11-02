// Package notify sends short messages to the places a person chose: a phone
// through ntfy or Telegram, a Discord or Slack channel, a webhook or a
// command of theirs. lmw uses it to tell about what its watches find.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Message is what a channel delivers.
type Message struct {
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	URL   string `json:"url,omitempty"`
	// Watch is the watch that found it, if any.
	Watch string `json:"watch,omitempty"`
}

// Channel is a named place notifications go to.
type Channel struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// Kinds lists the targets a channel can have, with an example of each.
var Kinds = []struct{ Scheme, Example, About string }{
	{"ntfy", "ntfy:my-secret-topic", "a push notification through ntfy.sh (or ntfy:https://your.server/topic)"},
	{"telegram", "telegram:123456:ABC-token:987654", "a Telegram message from your bot: telegram:BOT_TOKEN:CHAT_ID"},
	{"discord", "discord:https://discord.com/api/webhooks/...", "a message in a Discord channel, through its webhook"},
	{"slack", "slack:https://hooks.slack.com/services/...", "a message in a Slack channel, through its webhook"},
	{"webhook", "webhook:https://example.com/hook", "a JSON POST to your own URL: title, text, url, watch, time"},
	{"command", "command:notify-send \"$LMW_TITLE\" \"$LMW_TEXT\"", "a command in your shell, with LMW_TITLE, LMW_TEXT, LMW_URL and LMW_WATCH"},
}

// Validate checks that target is one lmw can send to.
func Validate(target string) error {
	scheme, rest, _ := strings.Cut(target, ":")
	switch scheme {
	case "ntfy":
		if strings.HasPrefix(rest, "http") {
			return checkURL(rest)
		}
		if rest == "" || strings.ContainsAny(rest, "/ ") {
			return errors.New("give the ntfy topic, as in ntfy:my-secret-topic")
		}
	case "telegram":
		token, chat := splitTelegram(rest)
		if !strings.Contains(token, ":") || chat == "" {
			return errors.New("give the bot token and the chat id, as in telegram:123456:ABC-token:987654")
		}
	case "discord", "slack", "webhook":
		return checkURL(rest)
	case "command":
		if strings.TrimSpace(rest) == "" {
			return errors.New("give the command to run, as in command:notify-send \"$LMW_TITLE\"")
		}
	default:
		return fmt.Errorf("unknown kind of channel %q; use ntfy, telegram, discord, slack, webhook or command", scheme)
	}
	return nil
}

// checkURL accepts https URLs, and http ones only on this computer.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not a URL", raw)
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return errors.New("use an https URL, so the notification is not sent in the clear")
	}
	return nil
}

// splitTelegram splits "BOT_TOKEN:CHAT_ID"; the token has a colon itself.
func splitTelegram(rest string) (token, chat string) {
	i := strings.LastIndex(rest, ":")
	if i < 0 {
		return rest, ""
	}
	return rest[:i], rest[i+1:]
}

// Mask hides the secret part of a target, for listing it.
func Mask(target string) string {
	scheme, rest, _ := strings.Cut(target, ":")
	switch scheme {
	case "telegram":
		token, chat := splitTelegram(rest)
		id, _, _ := strings.Cut(token, ":")
		return "telegram:" + id + ":…:" + chat
	case "ntfy", "discord", "slack", "webhook":
		if u, err := url.Parse(rest); err == nil && u.Host != "" {
			return scheme + ":" + u.Scheme + "://" + u.Host + "/…"
		}
		if len(rest) > 3 {
			return scheme + ":" + rest[:3] + "…"
		}
	}
	return target
}

// Store keeps the channels in a JSON file only the user can read, since
// targets hold tokens.
type Store struct{ Path string }

// Load returns the channels, none if the file does not exist yet.
func (s Store) Load() ([]Channel, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Channels []Channel `json:"channels"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w", s.Path, err)
	}
	return file.Channels, nil
}

// Save replaces the channels.
func (s Store) Save(channels []Channel) error {
	raw, err := json.MarshalIndent(map[string]any{"channels": channels}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Pick returns the channels a message goes to: the one named, or all of
// them when name is empty.
func Pick(channels []Channel, name string) ([]Channel, error) {
	if name == "" {
		return channels, nil
	}
	for _, c := range channels {
		if c.Name == name {
			return []Channel{c}, nil
		}
	}
	return nil, fmt.Errorf("there is no channel %q; see lmw notify list", name)
}

// Sender delivers messages. Its zero value works; tests replace its parts.
type Sender struct {
	HTTP *http.Client
	// TelegramAPI is the Bot API base URL.
	TelegramAPI string
	// Shell runs command targets; nil runs them in the system shell.
	Shell func(ctx context.Context, command string, env []string) error
	// Output receives what command targets print.
	Output io.Writer
}

// Send delivers m to target.
func (s Sender) Send(ctx context.Context, target string, m Message) error {
	if err := Validate(target); err != nil {
		return err
	}
	scheme, rest, _ := strings.Cut(target, ":")
	switch scheme {
	case "ntfy":
		endpoint := rest
		if !strings.HasPrefix(rest, "http") {
			endpoint = "https://ntfy.sh/" + rest
		}
		headers := map[string]string{"Title": m.Title, "Tags": "briefcase"}
		if m.URL != "" {
			headers["Click"] = m.URL
		}
		return s.post(ctx, endpoint, "text/plain; charset=utf-8", []byte(firstNonEmpty(m.Text, m.Title)), headers)
	case "telegram":
		token, chat := splitTelegram(rest)
		api := firstNonEmpty(s.TelegramAPI, "https://api.telegram.org")
		body, _ := json.Marshal(map[string]any{"chat_id": chat, "text": plain(m), "disable_web_page_preview": true})
		return s.post(ctx, api+"/bot"+token+"/sendMessage", "application/json", body, nil)
	case "discord":
		body, _ := json.Marshal(map[string]any{"content": plain(m)})
		return s.post(ctx, rest, "application/json", body, nil)
	case "slack":
		body, _ := json.Marshal(map[string]any{"text": plain(m)})
		return s.post(ctx, rest, "application/json", body, nil)
	case "webhook":
		body, _ := json.Marshal(map[string]any{"title": m.Title, "text": m.Text, "url": m.URL, "watch": m.Watch, "time": time.Now().Format(time.RFC3339)})
		return s.post(ctx, rest, "application/json", body, nil)
	default: // command
		env := []string{"LMW_TITLE=" + m.Title, "LMW_TEXT=" + m.Text, "LMW_URL=" + m.URL, "LMW_WATCH=" + m.Watch}
		if s.Shell != nil {
			return s.Shell(ctx, rest, env)
		}
		return runShell(ctx, rest, env, s.Output)
	}
}

func (s Sender) post(ctx context.Context, endpoint, contentType string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		// Header values must be one line.
		req.Header.Set(k, strings.Join(strings.Fields(v), " "))
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// The error may quote the URL, which can hold a token.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("could not reach %s: %v", hostOf(endpoint), urlErr.Err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s answered %s: %s", hostOf(endpoint), resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return "the server"
}

// plain writes a message as text for chat apps: title, text and link.
func plain(m Message) string {
	var lines []string
	for _, s := range []string{m.Title, m.Text, m.URL} {
		if s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// runShell runs command in the system shell with env added.
func runShell(ctx context.Context, command string, env []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	}
	cmd.Env = append(os.Environ(), env...)
	if out == nil {
		out = io.Discard
	}
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}
