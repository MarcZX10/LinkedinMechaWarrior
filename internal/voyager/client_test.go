package voyager

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/session"
)

type fakePacer struct {
	requests  int
	cooldowns []string
	block     error
}

func (p *fakePacer) BeforeRequest() error { return p.block }
func (p *fakePacer) RecordRequest() error { p.requests++; return nil }
func (p *fakePacer) StartCooldown(_ time.Duration, reason string) error {
	p.cooldowns = append(p.cooldowns, reason)
	return nil
}

type fakeResponse struct {
	status  int
	body    string
	headers map[string]string
	cookies []string
}

type fakeDoer struct {
	response fakeResponse
	requests []*http.Request
	bodies   []string
}

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	d.requests = append(d.requests, req)
	body := ""
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	d.bodies = append(d.bodies, body)
	header := http.Header{}
	for k, v := range d.response.headers {
		header.Set(k, v)
	}
	for _, c := range d.response.cookies {
		header.Add("Set-Cookie", c)
	}
	return &http.Response{
		StatusCode: d.response.status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(d.response.body)),
		Request:    req,
	}, nil
}

func newSession(t *testing.T) *session.Session {
	t.Helper()
	s, err := session.New(map[string]string{"li_at": "token", "JSESSIONID": `"ajax:42"`, "bcookie": `"v=2&b"`}, "test")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newClient(t *testing.T, response fakeResponse) (*Client, *fakeDoer, *fakePacer) {
	doer := &fakeDoer{response: response}
	pacer := &fakePacer{}
	return New(doer, pacer, identity.Default("windows"), newSession(t)), doer, pacer
}

func kindOf(err error) errs.Kind {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

func TestBuildURL(t *testing.T) {
	cases := []struct {
		path   string
		params Params
		want   string
	}{
		{"/me", nil, "https://www.linkedin.com/voyager/api/me"},
		{"voyager/api/me", nil, "https://www.linkedin.com/voyager/api/me"},
		{"/graphql", Params{{"variables", "(start:0,count:10)"}, {"queryId", "x.y"}, {"q", "a b&c=d+e"}},
			"https://www.linkedin.com/voyager/api/graphql?variables=(start:0,count:10)&queryId=x.y&q=a%20b%26c%3Dd%2Be"},
		{"/x?a=1", Params{{"b", "2"}}, "https://www.linkedin.com/voyager/api/x?a=1&b=2"},
	}
	for _, c := range cases {
		got, err := BuildURL(Origin, c.path, c.params)
		if err != nil || got != c.want {
			t.Errorf("BuildURL(%q) = %q, %v; want %q", c.path, got, err, c.want)
		}
	}
	for _, bad := range []string{"https://evil.example.com/voyager/api/me", "https://www.linkedin.com/feed/"} {
		if _, err := BuildURL(Origin, bad, nil); kindOf(err) != errs.Usage {
			t.Errorf("BuildURL(%q) should be rejected, got %v", bad, err)
		}
	}
}

func TestGetSendsBrowserHeaders(t *testing.T) {
	c, doer, pacer := newClient(t, fakeResponse{status: 200, body: `{"data":{"plainId":1}}`})

	raw, err := c.Get(context.Background(), "/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"data":{"plainId":1}}` || pacer.requests != 1 {
		t.Fatalf("unexpected result %s, %d requests", raw, pacer.requests)
	}

	req := doer.requests[0]
	want := map[string]string{
		"csrf-token":                "ajax:42",
		"cookie":                    `bcookie="v=2&b"; JSESSIONID="ajax:42"; li_at=token`,
		"x-restli-protocol-version": "2.0.0",
		"accept":                    NormalizedJSON,
		"sec-fetch-site":            "same-origin",
		"sec-ch-ua-platform":        `"Windows"`,
		"user-agent":                identity.Default("windows").UserAgent,
	}
	for name, value := range want {
		if got := req.Header[name]; len(got) != 1 || got[0] != value {
			t.Errorf("header %s = %v, want %q", name, got, value)
		}
	}
	if _, ok := req.Header["content-type"]; ok {
		t.Error("GET must not send content-type")
	}
	order := req.Header[http.HeaderOrderKey]
	if indexOf(order, "user-agent") > indexOf(order, "cookie") || indexOf(order, "csrf-token") < 0 {
		t.Errorf("unexpected header order %v", order)
	}
	for name := range req.Header {
		if name != http.HeaderOrderKey && indexOf(order, name) < 0 {
			t.Errorf("header %s missing from the order", name)
		}
	}
}

func TestPostSendsJSONBody(t *testing.T) {
	c, doer, _ := newClient(t, fakeResponse{status: 201})
	raw, err := c.Post(context.Background(), "/something", nil, map[string]int{"a": 1})
	if err != nil || string(raw) != "{}" {
		t.Fatalf("unexpected result %s %v", raw, err)
	}
	if doer.bodies[0] != `{"a":1}` {
		t.Errorf("body = %q", doer.bodies[0])
	}
	h := doer.requests[0].Header
	if h["content-type"][0] != "application/json; charset=UTF-8" || h["origin"][0] != Origin {
		t.Errorf("unexpected headers %v", h)
	}
}

func TestPacerCanBlockRequests(t *testing.T) {
	c, doer, pacer := newClient(t, fakeResponse{status: 200, body: "{}"})
	pacer.block = errs.New(errs.Budget, "over budget")
	if _, err := c.Get(context.Background(), "/me", nil); kindOf(err) != errs.Budget {
		t.Fatalf("expected a budget error, got %v", err)
	}
	if len(doer.requests) != 0 {
		t.Error("request was sent despite the pacer")
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		response fakeResponse
		kind     errs.Kind
		cooldown bool
	}{
		{"429", fakeResponse{status: 429}, errs.RateLimited, true},
		{"999", fakeResponse{status: 999}, errs.RateLimited, true},
		{"checkpoint redirect", fakeResponse{status: 302, headers: map[string]string{"Location": "https://www.linkedin.com/checkpoint/challenge/x"}}, errs.Challenge, true},
		{"challenge body", fakeResponse{status: 403, body: `{"status":403,"code":"CHALLENGE"}`}, errs.Challenge, true},
		{"login redirect", fakeResponse{status: 302, headers: map[string]string{"Location": "https://www.linkedin.com/login?x"}}, errs.AuthRequired, false},
		{"authwall redirect", fakeResponse{status: 303, headers: map[string]string{"Location": "/authwall?trk=x"}}, errs.AuthRequired, false},
		{"401", fakeResponse{status: 401}, errs.AuthRequired, false},
		{"csrf", fakeResponse{status: 403, body: "CSRF check failed."}, errs.AuthRequired, false},
		{"session deleted", fakeResponse{status: 200, body: "{}", cookies: []string{`li_at="delete me"; Max-Age=0; Path=/`}}, errs.AuthRequired, false},
		{"404", fakeResponse{status: 404, body: "not here"}, errs.API, false},
		{"html", fakeResponse{status: 200, body: "<html>login</html>", headers: map[string]string{"Content-Type": "text/html"}}, errs.API, false},
		{"bad json", fakeResponse{status: 200, body: "{nope"}, errs.API, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, pacer := newClient(t, tc.response)
			_, err := c.Get(context.Background(), "/me", nil)
			if kindOf(err) != tc.kind {
				t.Fatalf("expected kind %d, got %v", tc.kind, err)
			}
			if (len(pacer.cooldowns) > 0) != tc.cooldown {
				t.Errorf("cooldowns = %v", pacer.cooldowns)
			}
		})
	}

	c, _, _ := newClient(t, fakeResponse{status: 404, body: "not here"})
	_, err := c.Get(context.Background(), "/nope", nil)
	var httpErr *errs.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 404 {
		t.Errorf("expected an HTTPError, got %v", err)
	}
}

func TestSetCookieUpdatesTheSession(t *testing.T) {
	c, _, _ := newClient(t, fakeResponse{status: 200, body: "{}", cookies: []string{
		`lidc="b=VB:s=V"; Path=/; Domain=.linkedin.com; Secure`,
		`_tracking=1; Path=/`,
	}})
	if _, err := c.Get(context.Background(), "/me", nil); err != nil {
		t.Fatal(err)
	}
	if !c.SessionChanged() || c.Session().Cookies["lidc"] != `"b=VB:s=V"` {
		t.Errorf("session not updated: %+v", c.Session().Cookies)
	}
	if _, ok := c.Session().Cookies["_tracking"]; ok {
		t.Error("unknown cookie stored")
	}
}

func TestProfileFor(t *testing.T) {
	chrome := identity.Default("linux")
	if got := ProfileFor(chrome).GetClientHelloStr(); got != ProfileForName(t, "chrome_152") {
		t.Errorf("default identity should use chrome_152, got %s", got)
	}
	newer := identity.Identity{UserAgent: identity.ChromeUserAgent("linux", 160)}
	if got := ProfileFor(newer).GetClientHelloStr(); got != ProfileForName(t, "chrome_152") {
		t.Error("newer Chrome should use the newest Chrome profile")
	}
	firefox := identity.Identity{UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"}
	if got := ProfileFor(firefox).GetClientHelloStr(); got != ProfileForName(t, "firefox_135") {
		t.Error("Firefox 140 should use the newest Firefox profile not newer than 140")
	}
}

func ProfileForName(t *testing.T, name string) string {
	t.Helper()
	p, ok := profiles.MappedTLSClients[name]
	if !ok {
		t.Fatalf("profile %s does not exist", name)
	}
	return p.GetClientHelloStr()
}

// TestRealTLSClientEndToEnd runs the real tls-client against a local HTTPS
// server, to check HTTP/2, compression and cookies through the actual stack.
func TestRealTLSClientEndToEnd(t *testing.T) {
	var seen stdhttp.Header
	server := httptest.NewUnstartedServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		seen = r.Header.Clone()
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		_ = json.NewEncoder(gz).Encode(map[string]any{"data": map[string]any{"plainId": 7}})
		_ = gz.Close()
		w.Header().Set("Content-Type", "application/vnd.linkedin.normalized+json+2.1")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Set-Cookie", `lidc="b=new"; Path=/; Secure`)
		_, _ = w.Write(compressed.Bytes())
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	doer, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(ProfileFor(identity.Default("linux"))),
		tlsclient.WithInsecureSkipVerify(),
		tlsclient.WithNotFollowRedirects(),
	)
	if err != nil {
		t.Fatal(err)
	}
	c := New(doer, &fakePacer{}, identity.Default("linux"), newSession(t))
	c.origin = server.URL

	raw, err := c.Get(context.Background(), "/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]map[string]int
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded["data"]["plainId"] != 7 {
		t.Fatalf("unexpected body %s (%v)", raw, err)
	}
	if seen.Get("Csrf-Token") != "ajax:42" || !strings.Contains(seen.Get("Cookie"), "li_at=token") {
		t.Errorf("session headers not received: %v", seen)
	}
	if strings.Contains(seen.Get("User-Agent"), "Go-http-client") {
		t.Error("Go's default user agent leaked")
	}
	if c.Session().Cookies["lidc"] != `"b=new"` {
		t.Errorf("cookie not absorbed: %v", c.Session().Cookies)
	}
}

func indexOf(items []string, item string) int {
	for i, v := range items {
		if v == item {
			return i
		}
	}
	return -1
}
