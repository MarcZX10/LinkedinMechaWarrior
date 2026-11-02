package har

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

func entry(method, rawURL string, status int, headers [][2]string, body string, base64Body bool) map[string]any {
	var hs []map[string]string
	for _, h := range headers {
		hs = append(hs, map[string]string{"name": h[0], "value": h[1]})
	}
	content := map[string]any{"size": len(body), "mimeType": "application/json", "text": body}
	if base64Body {
		content["text"] = base64.StdEncoding.EncodeToString([]byte(body))
		content["encoding"] = "base64"
	}
	return map[string]any{
		"startedDateTime": "2026-10-01T10:00:00Z",
		"request":         map[string]any{"method": method, "url": rawURL, "headers": hs, "cookies": []any{}},
		"response":        map[string]any{"status": status, "content": content},
	}
}

func writeHAR(t *testing.T, entries ...map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"log": map[string]any{"version": "1.2", "entries": entries}})
	path := filepath.Join(t.TempDir(), "linkedin.har")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var browserHeaders = [][2]string{
	{":method", "GET"},
	{":authority", "www.linkedin.com"},
	{"sec-ch-ua-platform", `"Windows"`},
	{"csrf-token", "ajax:77"},
	{"user-agent", chromeUA},
	{"sec-ch-ua", `"Chromium";v="151", "Google Chrome";v="151", "Not.A/Brand";v="99"`},
	{"x-li-track", `{"clientVersion":"1.13.999","osName":"web"}`},
	{"accept", "application/vnd.linkedin.normalized+json+2.1"},
	{"accept-language", "es-ES,es;q=0.9,en;q=0.8"},
	{"cookie", `bcookie="v=2&x"; li_at=AQEDsecret; JSESSIONID="ajax:77"; _ga=1`},
}

func sampleHAR(t *testing.T) string {
	feed := `{"data":{"*elements":["urn:a"],"paging":{}},"included":[{"entityUrn":"urn:a","$type":"com.linkedin.voyager.feed.render.UpdateV2"},{"$type":"com.linkedin.voyager.feed.render.UpdateV2"}]}`
	return writeHAR(t,
		entry("GET", "https://www.linkedin.com/feed/", 200, nil, "<html>", false),
		entry("GET", "https://static.licdn.com/sc/h/x.js", 200, nil, "js", false),
		entry("GET", "https://www.linkedin.com/voyager/api/feed/updatesV2?count=10&q=chronFeed&start=0", 200, browserHeaders, feed, true),
		entry("POST", "https://www.linkedin.com/voyager/api/graphql?action=execute&queryId=messengerMessages.abc&variables=(conversationUrn:urn%3Ali%3Amsg_conversation%3A1)", 200, browserHeaders, `{"data":{}}`, false),
	)
}

func TestCallsListsOnlyVoyagerRequests(t *testing.T) {
	f, err := Load(sampleHAR(t))
	if err != nil {
		t.Fatal(err)
	}
	calls := f.Calls("")
	if len(calls) != 2 || calls[0].Index != 2 || calls[1].Index != 3 {
		t.Fatalf("unexpected calls %+v", calls)
	}
	feed := calls[0]
	if feed.Path != "/voyager/api/feed/updatesV2" || feed.Query[1].Name != "q" || feed.Query[1].Value != "chronFeed" {
		t.Errorf("unexpected call %+v", feed)
	}
	if feed.Types["UpdateV2"] != 2 || strings.Join(feed.DataKeys, ",") != "*elements,paging" {
		t.Errorf("response shape not summarized: %+v", feed)
	}
	if graphql := calls[1]; graphql.Query[2].Value != "(conversationUrn:urn:li:msg_conversation:1)" {
		t.Errorf("query values should be decoded: %+v", graphql.Query)
	}
	if got := f.Calls("graphql"); len(got) != 1 {
		t.Errorf("filter returned %d calls", len(got))
	}
	if !strings.Contains(feed.String(), "UpdateV2×2") {
		t.Errorf("listing = %q", feed.String())
	}
}

func TestIdentityCopiesBrowserHeadersInOrder(t *testing.T) {
	f, _ := Load(sampleHAR(t))
	id, err := f.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if id.UserAgent != chromeUA || id.AcceptLanguage != "es-ES,es;q=0.9,en;q=0.8" || id.Source != "har" {
		t.Errorf("unexpected identity %+v", id)
	}
	if !strings.Contains(id.LiTrack, "1.13.999") || id.LiLang != "en_US" {
		t.Errorf("x-li headers not copied: %+v", id)
	}
	if family, major := id.Browser(); family != "chrome" || major != 151 {
		t.Errorf("browser = %s %d", family, major)
	}
	wantOrder := "sec-ch-ua-platform,csrf-token,user-agent,sec-ch-ua,x-li-track,accept,accept-language,cookie"
	if got := strings.Join(id.HeaderOrder, ","); got != wantOrder {
		t.Errorf("header order = %s", got)
	}
}

func TestCookies(t *testing.T) {
	f, _ := Load(sampleHAR(t))
	cookies, err := f.Cookies()
	if err != nil {
		t.Fatal(err)
	}
	if cookies["li_at"] != "AQEDsecret" || cookies["JSESSIONID"] != `"ajax:77"` {
		t.Errorf("unexpected cookies %v", cookies)
	}

	sanitized, _ := Load(writeHAR(t, entry("GET", "https://www.linkedin.com/voyager/api/me", 200,
		[][2]string{{"user-agent", chromeUA}}, "{}", false)))
	if _, err := sanitized.Cookies(); err == nil || !strings.Contains(err.Error(), "sensitive data") {
		t.Errorf("expected a hint about sensitive data, got %v", err)
	}
}

func TestErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.har")); err == nil {
		t.Error("missing file should fail")
	}
	bad := filepath.Join(t.TempDir(), "bad.har")
	_ = os.WriteFile(bad, []byte("nope"), 0o600)
	if _, err := Load(bad); err == nil {
		t.Error("invalid file should fail")
	}
	empty, _ := Load(writeHAR(t))
	if _, err := empty.Identity(); err == nil {
		t.Error("identity from an empty HAR should fail")
	}
	if _, err := empty.Entry(0); err == nil {
		t.Error("out-of-range entry should fail")
	}
}

func TestIdentityRejectsBrowsersWithoutAFingerprint(t *testing.T) {
	safari := "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
	f, _ := Load(writeHAR(t, entry("GET", "https://www.linkedin.com/voyager/api/me", 200,
		[][2]string{{"user-agent", safari}}, "{}", false)))
	if _, err := f.Identity(); err == nil || !strings.Contains(err.Error(), "cannot imitate") {
		t.Errorf("expected a Safari identity to be rejected, got %v", err)
	}
}

func TestQueriesListsGraphQLQueriesOnce(t *testing.T) {
	entry := func(method, url, body string, status int) map[string]any {
		e := map[string]any{
			"request":  map[string]any{"method": method, "url": url, "headers": []any{}},
			"response": map[string]any{"status": status, "content": map[string]any{"text": "{}"}},
		}
		if body != "" {
			e["request"].(map[string]any)["postData"] = map[string]any{"mimeType": "application/json", "text": body}
		}
		return e
	}
	file := writeHAR(t,
		entry("GET", "https://www.linkedin.com/voyager/api/graphql?includeWebMetadata=true&variables=(count:10,start:0,threadUrn:urn%3Ali%3Aactivity%3A1)&queryId=voyagerSocialDashReactions.old", "", 200),
		entry("GET", "https://www.linkedin.com/voyager/api/graphql?includeWebMetadata=true&variables=(count:10,start:10,threadUrn:urn%3Ali%3Aactivity%3A1)&queryId=voyagerSocialDashReactions.new", "", 200),
		entry("POST", "https://www.linkedin.com/voyager/api/graphql?action=execute&queryId=voyagerSocialDashReactions.react",
			`{"variables":{"entity":{"reactionType":"LIKE"},"threadUrn":"urn:li:activity:1"},"queryId":"voyagerSocialDashReactions.react"}`, 200),
		entry("GET", "https://www.linkedin.com/voyager/api/voyagerMessagingGraphQL/graphql?queryId=messengerConversations.abc&variables=(query:(predicateUnions:List((conversationCategoryPredicate:(category:PRIMARY_INBOX)))),count:20,mailboxUrn:urn%3Ali%3Afsd_profile%3AACo)", "", 200),
		entry("GET", "https://www.linkedin.com/voyager/api/graphql?variables=(a:1)&queryId=broken.hash", "", 400),
	)
	f, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, q := range f.Queries() {
		got[q.Name+"("+strings.Join(q.Variables, ",")+")"] = q.Hash
	}
	want := map[string]string{
		"voyagerSocialDashReactions(count,start,threadUrn)": "new",
		"voyagerSocialDashReactions(entity,threadUrn)":      "react",
		"messengerConversations(count,mailboxUrn,query)":    "abc",
	}
	if len(got) != len(want) {
		t.Errorf("queries %v", got)
	}
	for key, hash := range want {
		if got[key] != hash {
			t.Errorf("%s: got hash %q, want %q (all: %v)", key, got[key], hash, got)
		}
	}
}
