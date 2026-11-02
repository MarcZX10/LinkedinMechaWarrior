package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/auth"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/config"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/pacing"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/session"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

const meJSON = `{"data":{"*miniProfile":"urn:li:fs_miniProfile:1"},"included":[{"entityUrn":"urn:li:fs_miniProfile:1","firstName":"Ada","lastName":"Lovelace","publicIdentifier":"ada"}]}`

const feedJSON = `{"data":{"*elements":["urn:u1"]},"included":[{"entityUrn":"urn:u1","updateMetadata":{"urn":"urn:li:activity:1"},"actor":{"name":{"text":"Grace Hopper"}},"commentary":{"text":{"text":"Hello"}}}]}`

type reply struct {
	status  int
	body    string
	cookies []string
}

// fakeLinkedIn answers requests by API path and records them.
type fakeLinkedIn struct {
	replies  map[string]reply
	requests []*http.Request
	bodies   []string
}

func (f *fakeLinkedIn) Do(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	body := ""
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	f.bodies = append(f.bodies, body)
	r, ok := f.replies[strings.TrimPrefix(req.URL.Path, voyager.APIPrefix)]
	if !ok {
		r = reply{status: 404, body: "not found"}
	}
	header := http.Header{"Content-Type": {"application/json"}}
	for _, c := range r.cookies {
		header.Add("Set-Cookie", c)
	}
	return &http.Response{StatusCode: r.status, Header: header, Body: io.NopCloser(strings.NewReader(r.body)), Request: req}, nil
}

type harness struct {
	t        *testing.T
	stateDir string
	linkedin *fakeLinkedIn
	sessions []auth.Candidate
	// browserAsked is the browser name passed to the cookie search.
	browserAsked string
}

func newHarness(t *testing.T) *harness {
	return &harness{
		t:        t,
		stateDir: t.TempDir(),
		linkedin: &fakeLinkedIn{replies: map[string]reply{
			"/me":                         {status: 200, body: meJSON},
			"/feed/updatesV2":             {status: 200, body: feedJSON},
			"/contentcreation/normShares": {status: 201, body: `{"data":{"*updateV2":"urn:li:fs_updateV2:(urn:li:activity:99,FEED_DETAIL)"}}`},
		}},
	}
}

// run executes lmw with args and stdin, returning the exit code, stdout and stderr.
func (h *harness) run(stdin string, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	a := &app{
		stdin:   bufio.NewReader(strings.NewReader(stdin)),
		stdinFD: -1,
		stdout:  &stdout,
		stderr:  &stderr,
		loadConfig: func() (config.Config, error) {
			return config.FromEnv(func(name string) string {
				return map[string]string{"LMW_STATE_DIR": h.stateDir, "LMW_SESSION_STORE": "file"}[name]
			})
		},
		newDoer: func(identity.Identity, time.Duration) (voyager.Doer, error) { return h.linkedin, nil },
		newPacer: func(cfg config.Config) *pacing.Pacer {
			p := defaultPacer(cfg)
			p.Sleep = func(time.Duration) {}
			return p
		},
		newStore: func(cfg config.Config) session.Store { return session.FileStore{Path: cfg.SessionFile()} },
		findSessions: func(_ context.Context, browser string) ([]auth.Candidate, error) {
			h.browserAsked = browser
			return h.sessions, nil
		},
	}
	code := a.run(context.Background(), args)
	return code, stdout.String(), stderr.String()
}

func (h *harness) mustRun(stdin string, args ...string) string {
	h.t.Helper()
	code, stdout, stderr := h.run(stdin, args...)
	if code != 0 {
		h.t.Fatalf("lmw %v exited %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout
}

func (h *harness) importPasted() {
	h.t.Helper()
	h.mustRun("AQEDsecret\najax:42\n", "auth", "import")
}

func TestVersionHelpAndUsageErrors(t *testing.T) {
	h := newHarness(t)
	if out := h.mustRun("", "--version"); !strings.HasPrefix(out, "lmw ") {
		t.Errorf("version output %q", out)
	}
	if out := h.mustRun("", "--help"); !strings.Contains(out, "auth") || !strings.Contains(out, "feed") {
		t.Errorf("help output %q", out)
	}
	for _, args := range [][]string{{"feed", "--nope"}, {"feed", "-n", "500"}, {"api", "get"}, {"api", "get", "/me", "-p", "bad"}, {"post", "draft"}} {
		if code, _, _ := h.run("", args...); code != 2 {
			t.Errorf("lmw %v exited %d, want 2", args, code)
		}
	}
	if len(h.linkedin.requests) != 0 {
		t.Error("usage errors must not send requests")
	}
}

func TestCommandsRequireASession(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("", "me")
	if code != 1 || !strings.Contains(stderr, "lmw auth import") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestImportPastedCookiesThenRead(t *testing.T) {
	h := newHarness(t)
	h.importPasted()

	raw, err := os.ReadFile(filepath.Join(h.stateDir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored session.Session
	_ = json.Unmarshal(raw, &stored)
	if stored.Cookies["li_at"] != "AQEDsecret" || stored.Cookies["JSESSIONID"] != `"ajax:42"` {
		t.Errorf("unexpected stored session %+v", stored)
	}
	if got := h.linkedin.requests[0].Header["csrf-token"]; len(got) != 1 || got[0] != "ajax:42" {
		t.Errorf("verification request csrf-token = %v", got)
	}

	if out := h.mustRun("", "me"); !strings.Contains(out, "Ada Lovelace") || !strings.Contains(out, "/in/ada/") {
		t.Errorf("me output %q", out)
	}
	var posts []map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("", "feed", "--json", "-n", "1")), &posts); err != nil || posts[0]["author"] != "Grace Hopper" {
		t.Errorf("feed json %v %v", posts, err)
	}
	if out := h.mustRun("", "auth", "status"); !strings.Contains(out, "Logged in as Ada Lovelace") || !strings.Contains(out, "pasted cookies") {
		t.Errorf("status output %q", out)
	}
}

func TestImportIsNotSavedWhenVerificationFails(t *testing.T) {
	h := newHarness(t)
	h.linkedin.replies["/me"] = reply{status: 401}
	code, _, stderr := h.run("AQEDsecret\najax:42\n", "auth", "import")
	if code != 1 || !strings.Contains(stderr, "not saved") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(h.stateDir, "session.json")); !os.IsNotExist(err) {
		t.Error("an unverified session was saved")
	}
}

func TestImportFromBrowserUsesNewestSession(t *testing.T) {
	h := newHarness(t)
	h.sessions = []auth.Candidate{
		{Browser: "firefox", Profile: "default", Cookies: map[string]string{"li_at": "new", "JSESSIONID": `"ajax:1"`}},
		{Browser: "chrome", Cookies: map[string]string{"li_at": "old", "JSESSIONID": `"ajax:2"`}},
	}
	out := h.mustRun("", "auth", "import", "--browser", "--no-verify")
	if !strings.Contains(out, "firefox (default), chrome") || !strings.Contains(out, "Session from firefox (default)") {
		t.Errorf("import output %q", out)
	}
	if len(h.linkedin.requests) != 0 {
		t.Error("--no-verify must not send requests")
	}

	for _, args := range [][]string{{"--browser", "firefox"}, {"--browser=firefox"}} {
		h.browserAsked = ""
		h.mustRun("", append([]string{"auth", "import", "--no-verify"}, args...)...)
		if h.browserAsked != "firefox" {
			t.Errorf("%v searched browser %q", args, h.browserAsked)
		}
	}
	if code, _, _ := h.run("", "auth", "import", "firefox"); code != 2 {
		t.Error("a browser name without --browser should be a usage error")
	}
}

func TestImportFromHARCopiesIdentity(t *testing.T) {
	h := newHarness(t)
	ua := "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
	harFile := filepath.Join(t.TempDir(), "linkedin.har")
	raw, _ := json.Marshal(map[string]any{"log": map[string]any{"entries": []any{map[string]any{
		"request": map[string]any{
			"method": "GET", "url": "https://www.linkedin.com/voyager/api/me",
			"headers": []any{
				map[string]string{"name": "User-Agent", "value": ua},
				map[string]string{"name": "Cookie", "value": `li_at=fromhar; JSESSIONID="ajax:9"`},
			},
		},
		"response": map[string]any{"status": 200, "content": map[string]any{"text": meJSON}},
	}}}})
	_ = os.WriteFile(harFile, raw, 0o600)

	out := h.mustRun("", "auth", "import", "--har", harFile)
	if !strings.Contains(out, "Firefox/140.0") || !strings.Contains(out, "Logged in as Ada Lovelace") {
		t.Errorf("import output %q", out)
	}
	if got := h.linkedin.requests[0].Header["user-agent"][0]; got != ua {
		t.Errorf("verification used user agent %q", got)
	}
	if _, ok := h.linkedin.requests[0].Header["sec-ch-ua"]; ok {
		t.Error("a Firefox identity must not send Chrome client hints")
	}
	if out := h.mustRun("", "identity", "show"); !strings.Contains(out, "firefox 140") || !strings.Contains(out, "har") {
		t.Errorf("identity output %q", out)
	}
	h.mustRun("", "identity", "reset")
	if out := h.mustRun("", "identity", "show"); !strings.Contains(out, "default") {
		t.Errorf("identity after reset %q", out)
	}
	if out := h.mustRun("", "har", "inspect", harFile); !strings.Contains(out, "/voyager/api/me") {
		t.Errorf("har inspect output %q", out)
	}
	if out := h.mustRun("", "har", "inspect", harFile, "--show", "0"); !strings.Contains(out, `"firstName": "Ada"`) {
		t.Errorf("har show output %q", out)
	}
}

func TestRefreshedCookiesAreSavedAndEndedSessionsRemoved(t *testing.T) {
	h := newHarness(t)
	h.importPasted()

	h.linkedin.replies["/me"] = reply{status: 200, body: meJSON, cookies: []string{`lidc="b=new"; Path=/`}}
	h.mustRun("", "me")
	raw, _ := os.ReadFile(filepath.Join(h.stateDir, "session.json"))
	if !strings.Contains(string(raw), `b=new`) {
		t.Errorf("refreshed cookie not saved: %s", raw)
	}

	h.linkedin.replies["/me"] = reply{status: 200, body: "{}", cookies: []string{`li_at=delete me; Max-Age=0; Path=/`}}
	if code, _, stderr := h.run("", "me"); code != 1 || !strings.Contains(stderr, "ended the session") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(h.stateDir, "session.json")); !os.IsNotExist(err) {
		t.Error("ended session was kept")
	}
}

func TestPublishNeedsConfirmation(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	before := len(h.linkedin.requests)

	out := h.mustRun("no\n", "post", "publish", "--text", "Hello LinkedIn")
	if !strings.Contains(out, "nothing was published") || len(h.linkedin.requests) != before {
		t.Errorf("cancelled publish sent requests or said %q", out)
	}

	out = h.mustRun("publish\n", "post", "publish", "--text", "Hello LinkedIn", "--connections-only")
	if !strings.Contains(out, "Published: https://www.linkedin.com/feed/update/urn:li:activity:99/") {
		t.Errorf("publish output %q", out)
	}
	last := h.linkedin.bodies[len(h.linkedin.bodies)-1]
	if !strings.Contains(last, `"text":"Hello LinkedIn"`) || !strings.Contains(last, `"visibleToConnectionsOnly":true`) {
		t.Errorf("publish payload %s", last)
	}

	if code, _, stderr := h.run("publish\n", "post", "publish", "--text", "Hi [fill this in]"); code != 1 || !strings.Contains(stderr, "placeholder") {
		t.Errorf("placeholder post: exit %d, stderr %q", code, stderr)
	}
}

func TestPublishJSONKeepsStdoutClean(t *testing.T) {
	h := newHarness(t)
	h.importPasted()

	code, stdout, stderr := h.run("publish\n", "post", "publish", "--text", "Hello LinkedIn", "--json")
	var post map[string]any
	if err := json.Unmarshal([]byte(stdout), &post); code != 0 || err != nil || post["urn"] != "urn:li:activity:99" {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if !strings.Contains(stderr, "Hello LinkedIn") || !strings.Contains(stderr, "Visible to") {
		t.Errorf("the post must still be shown before confirming, stderr %q", stderr)
	}

	if _, stdout, stderr := h.run("no\n", "post", "publish", "--text", "Hello LinkedIn", "--json"); stdout != "" || !strings.Contains(stderr, "nothing was published") {
		t.Errorf("cancelled: stdout %q, stderr %q", stdout, stderr)
	}
}

func TestOfflinePostCommands(t *testing.T) {
	h := newHarness(t)
	file := filepath.Join(t.TempDir(), "post.txt")
	out := h.mustRun("", "post", "draft", "Shipping", "a", "CLI", "--tone", "technical", "-o", file)
	if !strings.Contains(out, "A technical lesson worth sharing: Shipping a CLI.") {
		t.Errorf("draft output %q", out)
	}
	if code, out, _ := h.run("", "post", "preview", "--text-file", file); code != 1 || !strings.Contains(out, "Unfilled placeholder") {
		t.Errorf("preview of an outline: exit %d, %q", code, out)
	}
	if out := h.mustRun("", "post", "preview", "--text", "All done. Thoughts?"); !strings.Contains(out, "Ready to publish") {
		t.Errorf("preview output %q", out)
	}
	if code, _, _ := h.run("", "post", "preview", "--text", "a", "--text-file", file); code != 2 {
		t.Error("--text and --text-file together should be a usage error")
	}
	if code, _, _ := h.run("", "post", "publish"); code != 2 {
		t.Error("publishing without text should be a usage error")
	}
	if len(h.linkedin.requests) != 0 {
		t.Error("offline commands sent requests")
	}
}

func TestRateLimitStartsACooldownThatBlocksLaterCommands(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/feed/updatesV2"] = reply{status: 999}

	if code, _, stderr := h.run("", "feed"); code != 1 || !strings.Contains(stderr, "rate-limited") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	sent := len(h.linkedin.requests)
	if code, _, stderr := h.run("", "me"); code != 1 || !strings.Contains(stderr, "paused") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if len(h.linkedin.requests) != sent {
		t.Error("a request was sent during the cooldown")
	}

	var usage map[string]any
	_ = json.Unmarshal([]byte(h.mustRun("", "limits", "--json")), &usage)
	// The import check and the rate-limited feed request; nothing during the cooldown.
	if usage["cooldown_until"] == nil || usage["last_day"].(float64) != 2 {
		t.Errorf("limits %v", usage)
	}
	var cleared map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("", "limits", "--clear-cooldown", "--json")), &cleared); err != nil || cleared["cooldown_until"] != nil {
		t.Errorf("limits --clear-cooldown --json: %v, %v", err, cleared)
	}
	h.mustRun("", "me")
}

func TestAPIGetPassesParameters(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	out := h.mustRun("", "api", "get", "/me", "-p", "a=1", "-p", "v=(x:y)")
	if !strings.Contains(out, `"firstName": "Ada"`) {
		t.Errorf("api get output %q", out)
	}
	last := h.linkedin.requests[len(h.linkedin.requests)-1]
	if last.URL.RawQuery != "a=1&v=(x:y)" {
		t.Errorf("query %q", last.URL.RawQuery)
	}
}

func TestLogoutNeedsConfirmation(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.mustRun("no\n", "auth", "logout")
	if _, err := os.Stat(filepath.Join(h.stateDir, "session.json")); err != nil {
		t.Fatal("cancelled logout removed the session")
	}
	h.mustRun("logout\n", "auth", "logout")
	if _, err := os.Stat(filepath.Join(h.stateDir, "session.json")); !os.IsNotExist(err) {
		t.Error("logout kept the session")
	}
}

func TestInterruptibleSleepStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	interruptibleSleep(ctx)(time.Minute)
	if time.Since(start) > time.Second {
		t.Error("sleep was not interrupted")
	}
}

// mcpSession runs `lmw mcp` with the given JSON-RPC lines and returns the
// answers by id.
func (h *harness) mcpSession(flags []string, lines ...string) map[float64]map[string]any {
	h.t.Helper()
	out := h.mustRun(strings.Join(lines, "\n")+"\n", append([]string{"mcp"}, flags...)...)
	answers := map[float64]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			h.t.Fatalf("stdout must only carry MCP messages, got %q", line)
		}
		id, _ := msg["id"].(float64)
		answers[id] = msg
	}
	return answers
}

func mcpText(t *testing.T, msg map[string]any) (string, bool) {
	t.Helper()
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", msg)
	}
	return result["content"].([]any)[0].(map[string]any)["text"].(string), result["isError"] == true
}

func TestMCPServesReadToolsAndGuardsWrites(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	before := len(h.linkedin.requests)
	answers := h.mcpSession(nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_my_profile","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_feed","arguments":{"limit":1}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"publish_post","arguments":{"text":"Hello"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"linkedin_api_get","arguments":{"path":"/me","query":"a=1"}}}`,
	)
	if info := answers[1]["result"].(map[string]any)["serverInfo"].(map[string]any); info["name"] != "lmw" {
		t.Errorf("server info %v", info)
	}
	for _, tool := range answers[2]["result"].(map[string]any)["tools"].([]any) {
		if tool.(map[string]any)["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Errorf("write tool offered without --allow-write: %v", tool.(map[string]any)["name"])
		}
	}
	if text, isError := mcpText(t, answers[3]); isError || !strings.Contains(text, `"name": "Ada Lovelace"`) {
		t.Errorf("get_my_profile: %s", text)
	}
	if text, isError := mcpText(t, answers[4]); isError || !strings.Contains(text, `"author": "Grace Hopper"`) {
		t.Errorf("get_feed: %s", text)
	}
	if text, isError := mcpText(t, answers[5]); !isError || !strings.Contains(text, "--allow-write") {
		t.Errorf("publish_post without --allow-write: %s", text)
	}
	if text, isError := mcpText(t, answers[6]); isError || !strings.Contains(text, `"firstName": "Ada"`) {
		t.Errorf("linkedin_api_get: %s", text)
	}
	for _, req := range h.linkedin.requests[before:] {
		if req.Method != "GET" {
			t.Errorf("a read-only session sent %s %s", req.Method, req.URL)
		}
	}

	answers = h.mcpSession([]string{"--allow-write"},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"publish_post","arguments":{"text":"Hi [fill this in]"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish_post","arguments":{"text":"Hello LinkedIn","visibility":"connections"}}}`,
	)
	if text, isError := mcpText(t, answers[2]); !isError || !strings.Contains(text, "placeholder") {
		t.Errorf("placeholder post: %s", text)
	}
	if text, isError := mcpText(t, answers[3]); isError || !strings.Contains(text, "urn:li:activity:99") {
		t.Errorf("publish_post: %s", text)
	}
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"visibleToConnectionsOnly":true`) {
		t.Errorf("publish payload %s", last)
	}
}

const dashProfileJSON = `{"data":{"*elements":["urn:li:fsd_profile:ACoAAB"]},"included":[` +
	`{"$type":"com.linkedin.voyager.dash.identity.profile.Profile","entityUrn":"urn:li:fsd_profile:ACoAAB","firstName":"Grace","lastName":"Hopper","headline":"Rear admiral","publicIdentifier":"grace"},` +
	`{"$type":"com.linkedin.voyager.dash.identity.profile.Position","title":"Programmer","companyName":"Harvard","dateRange":{"start":{"year":1944}}},` +
	`{"$type":"com.linkedin.voyager.dash.identity.profile.Skill","name":"COBOL"}]}`

func TestProfileCommands(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/identity/dash/profiles"] = reply{status: 200, body: dashProfileJSON}
	h.linkedin.replies["/graphql"] = reply{status: 200, body: feedJSON}
	h.linkedin.replies["/flagship-web/rsc-action/actions/navigation"] = reply{status: 200, body: `1:{"viewName":"profile-contact-info-details-view"}` +
		`{"viewName":"contact-email"}["$","p",null,{"className":"x","children":["Email"]}]{"urlValue":{"$case":"url","url":"mailto:grace@example.com"}}`}

	out := h.mustRun("", "profile", "show", "https://www.linkedin.com/in/grace/")
	for _, want := range []string{"Grace Hopper", "Rear admiral", "Programmer · Harvard", "1944 – present", "COBOL"} {
		if !strings.Contains(out, want) {
			t.Errorf("profile show is missing %q:\n%s", want, out)
		}
	}
	if last := h.linkedin.requests[len(h.linkedin.requests)-1]; last.URL.Query().Get("memberIdentity") != "grace" {
		t.Errorf("profile show asked for %s", last.URL)
	}
	if out := h.mustRun("", "profile", "contact", "grace"); !strings.Contains(out, "grace@example.com") {
		t.Errorf("profile contact: %s", out)
	}
	var posts []map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("", "profile", "posts", "grace", "-n", "1", "--json")), &posts); err != nil || posts[0]["author"] != "Grace Hopper" {
		t.Errorf("profile posts: %v %v", posts, err)
	}
	if code, _, stderr := h.run("", "profile", "show", "not a profile"); code != 2 || !strings.Contains(stderr, "not a LinkedIn profile") {
		t.Errorf("bad profile: exit %d, %s", code, stderr)
	}
}

const invitationsJSON = `{"data":{"*elements":["urn:li:fs_relInvitationView:1"]},"included":[` +
	`{"entityUrn":"urn:li:fs_relInvitationView:1","*invitation":"urn:li:fs_relInvitation:7001"},` +
	`{"entityUrn":"urn:li:fs_relInvitation:7001","sharedSecret":"s3cret","message":"Hi!","*fromMember":"urn:li:fs_miniProfile:ACoAAG"},` +
	`{"entityUrn":"urn:li:fs_miniProfile:ACoAAG","firstName":"Grace","lastName":"Hopper","publicIdentifier":"grace"}]}`

func TestInvitationsAndConnections(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/relationships/invitationViews"] = reply{status: 200, body: invitationsJSON}
	h.linkedin.replies["/relationships/invitations/7001"] = reply{status: 200, body: `{}`}
	h.linkedin.replies["/identity/dash/profiles"] = reply{status: 200, body: dashProfileJSON}
	h.linkedin.replies["/voyagerRelationshipsDashMemberRelationships"] = reply{status: 200, body: `{}`}

	if out := h.mustRun("", "invitations"); !strings.Contains(out, "Grace Hopper") || !strings.Contains(out, "id 7001") || !strings.Contains(out, "Hi!") {
		t.Errorf("invitations: %s", out)
	}
	before := len(h.linkedin.requests)
	if h.mustRun("no\n", "invitations", "accept", "7001"); len(h.linkedin.requests) != before+1 {
		t.Errorf("a cancelled accept may only read the list: %d requests", len(h.linkedin.requests)-before)
	}
	out := h.mustRun("accept\n", "invitations", "accept", "7001")
	if !strings.Contains(out, "Accepted the invitation from Grace Hopper") {
		t.Errorf("accept: %s", out)
	}
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"invitationSharedSecret":"s3cret"`) {
		t.Errorf("accept payload %s", last)
	}

	before = len(h.linkedin.requests)
	if _, out, _ := h.run("no\n", "connect", "grace", "--note", "Hello"); !strings.Contains(out, "Hello") || len(h.linkedin.requests) != before {
		t.Errorf("a cancelled connect sent requests or did not show the note: %s", out)
	}
	h.mustRun("connect\n", "connect", "grace", "--note", "Hello")
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"memberProfile":"urn:li:fsd_profile:ACoAAB"`) {
		t.Errorf("connect payload %s", last)
	}
	if code, _, _ := h.run("", "connect", "grace", "--note", strings.Repeat("x", 301)); code != 1 {
		t.Errorf("a long note should fail, exit %d", code)
	}
}

func TestHARLearnSavesQueriesThatLaterRequestsUse(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	harFile := filepath.Join(t.TempDir(), "linkedin.har")
	raw, _ := json.Marshal(map[string]any{"log": map[string]any{"entries": []any{map[string]any{
		"request": map[string]any{
			"method":  "GET",
			"url":     "https://www.linkedin.com/voyager/api/graphql?includeWebMetadata=true&variables=(count:10,start:0,profileUrn:urn%3Ali%3Afsd_profile%3AACo)&queryId=voyagerFeedDashProfileUpdates.fresh123",
			"headers": []any{},
		},
		"response": map[string]any{"status": 200, "content": map[string]any{"text": "{}"}},
	}}}})
	_ = os.WriteFile(harFile, raw, 0o600)

	if out := h.mustRun("", "har", "learn", harFile); !strings.Contains(out, "voyagerFeedDashProfileUpdates(count, profileUrn, start) fresh123") {
		t.Errorf("har learn: %s", out)
	}
	h.linkedin.replies["/graphql"] = reply{status: 200, body: feedJSON}
	h.mustRun("", "profile", "posts", "ACoAABcdEfGh123456789012345", "-n", "1")
	if q := h.linkedin.requests[len(h.linkedin.requests)-1].URL.Query().Get("queryId"); q != "voyagerFeedDashProfileUpdates.fresh123" {
		t.Errorf("the learned version was not tried first: %s", q)
	}
}

func TestPostInteractions(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	post := "https://www.linkedin.com/feed/update/urn:li:activity:42/"
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{"data":{"data":{"socialDashCommentsBySocialDetail":{"*elements":["urn:li:fsd_comment:(1,urn:li:activity:42)"]}}},"included":[` +
		`{"entityUrn":"urn:li:fsd_comment:(1,urn:li:activity:42)","commenter":{"title":{"text":"Grace Hopper"}},"commentary":{"text":"Nice one"}}]}`}
	if out := h.mustRun("", "post", "comments", post); !strings.Contains(out, "Grace Hopper") || !strings.Contains(out, "Nice one") {
		t.Errorf("post comments: %s", out)
	}

	before := len(h.linkedin.requests)
	h.mustRun("no\n", "post", "react", post, "--type", "celebrate")
	if len(h.linkedin.requests) != before {
		t.Error("a cancelled reaction sent a request")
	}
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{}`}
	if out := h.mustRun("react\n", "post", "react", post, "--type", "celebrate"); !strings.Contains(out, "Reacted celebrate") {
		t.Errorf("post react: %s", out)
	}
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"reactionType":"PRAISE"`) {
		t.Errorf("react payload %s", last)
	}
	if code, _, stderr := h.run("", "post", "react", post, "--type", "meh"); code != 2 || !strings.Contains(stderr, "celebrate") {
		t.Errorf("unknown reaction: exit %d, %s", code, stderr)
	}

	h.linkedin.replies["/voyagerSocialDashNormComments"] = reply{status: 201, body: `{}`}
	if out := h.mustRun("comment\n", "post", "comment", post, "--text", "Well said"); !strings.Contains(out, "Comment posted") {
		t.Errorf("post comment: %s", out)
	}
	h.linkedin.replies["/contentcreation/normShares/urn:li:activity:42"] = reply{status: 204}
	if out := h.mustRun("delete\n", "post", "delete", "urn:li:activity:42"); !strings.Contains(out, "Post deleted") {
		t.Errorf("post delete: %s", out)
	}
	if last := h.linkedin.requests[len(h.linkedin.requests)-1]; last.Method != "DELETE" {
		t.Errorf("delete sent %s", last.Method)
	}
}

func TestMessagesCommands(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/voyagerMessagingGraphQL/graphql"] = reply{status: 200, body: `{"data":{"data":{"messengerConversationsByCategoryQuery":{"*elements":["urn:li:msg_conversation:(urn:li:fsd_profile:1,2-abc==)"]}}},"included":[` +
		`{"entityUrn":"urn:li:msg_conversation:(urn:li:fsd_profile:1,2-abc==)","title":"Grace Hopper","unreadCount":1}]}`}
	if out := h.mustRun("", "messages"); !strings.Contains(out, "• Grace Hopper") || !strings.Contains(out, "id 2-abc==") {
		t.Errorf("messages: %s", out)
	}
	h.linkedin.replies["/voyagerMessagingDashMessengerMessages"] = reply{status: 200, body: `{}`}
	if code, _, _ := h.run("", "messages", "send", "--text", "Hi"); code != 2 {
		t.Error("send needs --to or --conversation")
	}
	before := len(h.linkedin.requests)
	h.mustRun("no\n", "messages", "send", "--conversation", "2-abc==", "--text", "Hi")
	if len(h.linkedin.requests) != before {
		t.Error("a cancelled message sent requests")
	}
	if out := h.mustRun("send\n", "messages", "send", "--conversation", "2-abc==", "--text", "Hi Grace"); !strings.Contains(out, "Message sent") {
		t.Errorf("messages send: %s", out)
	}
	if last := h.linkedin.bodies[len(h.linkedin.bodies)-1]; !strings.Contains(last, `"text":"Hi Grace"`) || !strings.Contains(last, "2-abc==") {
		t.Errorf("message payload %s", last)
	}
}

func TestSearchCompanyAndJobs(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{"data":{"data":{"searchDashClustersByAll":{"elements":[{"items":[{"item":{"*entityResult":"urn:li:r:1"}}]}]}}},"included":[` +
		`{"entityUrn":"urn:li:r:1","title":{"text":"Grace Hopper"},"primarySubtitle":{"text":"Admiral"},"navigationUrl":"https://www.linkedin.com/in/grace?x=1"}]}`}
	if out := h.mustRun("", "search", "people", "grace", "--network", "2nd"); !strings.Contains(out, "Grace Hopper") || !strings.Contains(out, "https://www.linkedin.com/in/grace\n") {
		t.Errorf("search: %s", out)
	}
	if code, _, _ := h.run("", "search", "posts", "go"); code != 2 {
		t.Error("searching posts should be a usage error")
	}

	h.linkedin.replies["/organization/companies"] = reply{status: 200, body: `{"data":{"*elements":["urn:li:fs_normalized_company:7"]},"included":[` +
		`{"$type":"com.linkedin.voyager.organization.Company","entityUrn":"urn:li:fs_normalized_company:7","name":"Acme","universalName":"acme","tagline":"We make things"}]}`}
	if out := h.mustRun("", "company", "show", "https://www.linkedin.com/company/acme/"); !strings.Contains(out, "Acme") || !strings.Contains(out, "We make things") {
		t.Errorf("company show: %s", out)
	}
	h.linkedin.replies["/feed/dash/followingStates/urn:li:fsd_followingState:urn:li:fsd_company:7"] = reply{status: 200}
	if out := h.mustRun("follow\n", "company", "follow", "acme"); !strings.Contains(out, "Followed acme") {
		t.Errorf("company follow: %s", out)
	}

	h.linkedin.replies["/voyagerJobsDashJobCards"] = reply{status: 200, body: `{"data":{"*elements":[]},"included":[` +
		`{"$type":"com.linkedin.voyager.dash.jobs.JobPostingCard","entityUrn":"urn:li:fsd_jobPostingCard:(4012345678,JOBS_SEARCH)","jobPostingTitle":"Go engineer","primaryDescription":{"text":"Acme"},"*jobPosting":"urn:li:fsd_jobPosting:4012345678"}]}`}
	var jobs []map[string]any
	if err := json.Unmarshal([]byte(h.mustRun("", "jobs", "search", "go", "--workplace", "remote", "--json")), &jobs); err != nil || len(jobs) != 1 || jobs[0]["id"] != "4012345678" {
		t.Errorf("jobs search: %v %v", jobs, err)
	}
	h.linkedin.replies["/jobs/jobPostings/4012345678"] = reply{status: 200, body: `{"data":{"title":"Go engineer","formattedLocation":"Barcelona","description":{"text":"Build CLIs."}},"included":[]}`}
	if out := h.mustRun("", "jobs", "show", "https://www.linkedin.com/jobs/view/4012345678/"); !strings.Contains(out, "Build CLIs.") {
		t.Errorf("jobs show: %s", out)
	}
}

func TestMCPOffersEveryFeature(t *testing.T) {
	h := newHarness(t)
	answers := h.mcpSession([]string{"--allow-write"},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	names := map[string]bool{}
	for _, tool := range answers[2]["result"].(map[string]any)["tools"].([]any) {
		t := tool.(map[string]any)
		names[t["name"].(string)] = true
	}
	for _, want := range []string{
		"get_my_profile", "get_feed", "get_profile", "get_profile_posts", "get_contact_info", "get_post", "get_post_comments",
		"get_post_reactions", "list_conversations", "get_conversation", "get_notifications", "list_invitations", "list_connections",
		"search", "get_company", "search_jobs", "get_job", "get_saved_posts", "get_tracked_jobs", "get_request_budget", "linkedin_api_get",
		"publish_post", "edit_post", "comment_on_post", "reply_to_comment", "react_to_post", "remove_reaction", "delete_post", "send_message", "mark_conversation_read",
		"answer_invitation", "withdraw_invitation", "send_connection_request", "remove_connection", "set_following",
	} {
		if !names[want] {
			t.Errorf("tool %s is missing", want)
		}
	}
}

func TestSavedPostsAndTrackedJobs(t *testing.T) {
	h := newHarness(t)
	h.importPasted()
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{"data":{"data":{"searchDashClustersByAll":{"elements":[{"items":[{"item":{"*entityResult":"urn:li:r:1"}}]}]}}},"included":[` +
		`{"entityUrn":"urn:li:r:1","trackingUrn":"urn:li:fsd_entityResultViewModel:(urn:li:activity:42,SEARCH_MY_ITEMS_SAVED_POSTS,DEFAULT)","title":{"text":"Grace Hopper"},"summary":{"text":"On compilers"}}]}`}
	if out := h.mustRun("", "saved"); !strings.Contains(out, "Grace Hopper") || !strings.Contains(out, "urn:li:activity:42") {
		t.Errorf("saved: %s", out)
	}
	job := "urn:li:fsd_entityResultViewModel:(urn:li:fsd_jobPosting:4012345678,SEARCH_MY_ITEMS_JOB_SEEKER,DEFAULT)"
	h.linkedin.replies["/graphql"] = reply{status: 200, body: `{"data":{"data":{"searchDashClustersByAll":{"elements":[{"items":[{"item":{"*entityResult":"` + job + `"}}]}]}}},"included":[` +
		`{"entityUrn":"` + job + `","title":{"text":"Go engineer"},"primarySubtitle":{"text":"Acme"}}]}`}
	if out := h.mustRun("", "jobs", "tracked", "--stage", "applied"); !strings.Contains(out, "Go engineer   id 4012345678") {
		t.Errorf("jobs tracked: %s", out)
	}
	if code, _, _ := h.run("", "jobs", "tracked", "--stage", "nope"); code != 2 {
		t.Error("an unknown stage should be a usage error")
	}
}
