// Package voyager is a client for LinkedIn's internal "Voyager" web API.
//
// Requests are plain HTTP, sent with tls-client so that the TLS and HTTP/2
// fingerprint match the browser named by the identity, and with the same
// headers LinkedIn's web app sends from that browser. Every request goes
// through the pacer, and any sign of pushback from LinkedIn stops the run.
package voyager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/session"
)

const (
	// Origin is LinkedIn's web origin.
	Origin = "https://www.linkedin.com"
	// APIPrefix is the path prefix of the internal API.
	APIPrefix = "/voyager/api"
	// NormalizedJSON asks for responses with entities in a flat "included" list.
	NormalizedJSON = "application/vnd.linkedin.normalized+json+2.1"

	// RateLimitCooldown pauses requests after HTTP 429 or 999.
	RateLimitCooldown = time.Hour
	// ChallengeCooldown pauses requests after a security challenge.
	ChallengeCooldown = 6 * time.Hour

	maxResponseBytes = 32 << 20
)

var (
	loginMarkers     = []string{"/login", "/authwall", "/uas/", "/signup"}
	challengeMarkers = []string{"/checkpoint", "/challenge"}
	// Characters Rest.li needs verbatim in query values. "%" is kept so callers
	// can pass values that are already encoded, such as URNs inside `(...)`.
	querySafe = "(),:%"
)

// Doer sends HTTP requests; tls-client's HttpClient implements it.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Pacer is the subset of pacing.Pacer the client needs.
type Pacer interface {
	BeforeRequest() error
	RecordRequest() error
	StartCooldown(time.Duration, string) error
}

// Param is one query parameter; order is preserved.
type Param struct{ Key, Value string }

// Params is an ordered list of query parameters.
type Params []Param

// Client sends requests to the Voyager API.
type Client struct {
	doer     Doer
	pacer    Pacer
	identity identity.Identity
	session  *session.Session
	changed  bool

	// Origin is overridden in tests.
	origin string
	// Referer is the page LinkedIn's web app would be on when making the request.
	Referer string
}

// New creates a client.
func New(doer Doer, pacer Pacer, id identity.Identity, s *session.Session) *Client {
	return &Client{doer: doer, pacer: pacer, identity: id, session: s, origin: Origin, Referer: Origin + "/feed/"}
}

// NewHTTPDoer creates a tls-client HTTP client whose fingerprint matches the identity.
func NewHTTPDoer(id identity.Identity, timeout time.Duration) (Doer, error) {
	return tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(ProfileFor(id)),
		tlsclient.WithTimeoutSeconds(int(timeout.Seconds())),
		tlsclient.WithNotFollowRedirects(),
	)
}

var profileName = regexp.MustCompile(`^(chrome|firefox)_(\d+)$`)

// ProfileFor picks the newest TLS profile of the identity's browser family
// that is not newer than its version.
func ProfileFor(id identity.Identity) profiles.ClientProfile {
	family, major := id.Browser()
	bestVersion, bestName := -1, ""
	newestVersion, newestName := -1, ""
	for name := range profiles.MappedTLSClients {
		m := profileName.FindStringSubmatch(name)
		if m == nil || m[1] != family {
			continue
		}
		version, _ := strconv.Atoi(m[2])
		if version <= major && version > bestVersion {
			bestVersion, bestName = version, name
		}
		if version > newestVersion {
			newestVersion, newestName = version, name
		}
	}
	switch {
	case bestName != "":
		return profiles.MappedTLSClients[bestName]
	case newestName != "":
		return profiles.MappedTLSClients[newestName]
	default:
		return profiles.DefaultClientProfile
	}
}

// Session returns the (possibly updated) session.
func (c *Client) Session() *session.Session { return c.session }

// SessionChanged reports whether LinkedIn updated any stored cookie.
func (c *Client) SessionChanged() bool { return c.changed }

// BuildURL turns an API path and parameters into a full URL.
func BuildURL(origin, path string, params Params) (string, error) {
	var u string
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		if !strings.HasPrefix(path, origin+APIPrefix+"/") {
			return "", errs.New(errs.Usage, "only %s%s/ URLs are allowed, got %q", origin, APIPrefix, path)
		}
		u = path
	} else {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if !strings.HasPrefix(path, APIPrefix+"/") {
			path = APIPrefix + path
		}
		u = origin + path
	}
	if len(params) > 0 {
		parts := make([]string, len(params))
		for i, p := range params {
			parts[i] = url.QueryEscape(p.Key) + "=" + escapeValue(p.Value)
		}
		separator := "?"
		if strings.Contains(u, "?") {
			separator = "&"
		}
		u += separator + strings.Join(parts, "&")
	}
	return u, nil
}

func escapeValue(value string) string {
	var b strings.Builder
	for _, r := range value {
		if strings.ContainsRune(querySafe, r) {
			b.WriteRune(r)
			continue
		}
		// QueryEscape encodes spaces as "+", which Rest.li would read literally.
		b.WriteString(strings.ReplaceAll(url.QueryEscape(string(r)), "+", "%20"))
	}
	return b.String()
}

// Get sends a GET request and returns the raw JSON body.
func (c *Client) Get(ctx context.Context, path string, params Params) ([]byte, error) {
	return c.Do(ctx, "GET", path, params, nil, NormalizedJSON)
}

// Post sends a POST request with a JSON payload and returns the raw JSON body.
func (c *Client) Post(ctx context.Context, path string, params Params, payload any) ([]byte, error) {
	return c.Do(ctx, "POST", path, params, payload, NormalizedJSON)
}

// Delete sends a DELETE request and returns the raw JSON body, if any.
func (c *Client) Delete(ctx context.Context, path string, params Params) ([]byte, error) {
	return c.Do(ctx, "DELETE", path, params, nil, NormalizedJSON)
}

// Do sends one paced request.
func (c *Client) Do(ctx context.Context, method, path string, params Params, payload any, accept string) ([]byte, error) {
	if err := c.session.Validate(); err != nil {
		return nil, err
	}
	target, err := BuildURL(c.origin, path, params)
	if err != nil {
		return nil, err
	}
	var body []byte
	if payload != nil {
		if body, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body == nil {
		req.Body = nil
	}
	req.Header = c.headers(accept, body != nil)

	if err := c.pacer.BeforeRequest(); err != nil {
		return nil, err
	}
	if err := c.pacer.RecordRequest(); err != nil {
		return nil, err
	}
	slog.Debug("voyager request", "method", method, "url", target)
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, errs.Wrap(errs.API, err, "request to %s failed: %v", target, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, errs.Wrap(errs.API, err, "reading the response from %s failed: %v", target, err)
	}
	slog.Debug("voyager response", "status", resp.StatusCode, "bytes", len(raw))
	c.absorbCookies(resp)
	return c.handle(resp, raw, target)
}

func (c *Client) headers(accept string, hasBody bool) http.Header {
	id := c.identity
	values := map[string]string{
		"accept":                    accept,
		"accept-encoding":           "gzip, deflate, br, zstd",
		"accept-language":           id.AcceptLanguage,
		"cookie":                    c.session.CookieHeader(),
		"csrf-token":                c.session.CSRFToken(),
		"referer":                   c.Referer,
		"sec-fetch-dest":            "empty",
		"sec-fetch-mode":            "cors",
		"sec-fetch-site":            "same-origin",
		"user-agent":                id.UserAgent,
		"x-li-lang":                 id.LiLang,
		"x-restli-protocol-version": "2.0.0",
	}
	if family, _ := id.Browser(); family == "chrome" {
		values["priority"] = "u=1, i"
	}
	if id.SecCHUA != "" {
		values["sec-ch-ua"] = id.SecCHUA
		values["sec-ch-ua-mobile"] = id.SecCHUAMobile
		values["sec-ch-ua-platform"] = id.SecCHUAPlatform
	}
	if id.LiTrack != "" {
		values["x-li-track"] = id.LiTrack
	}
	if hasBody {
		values["content-type"] = "application/json; charset=UTF-8"
		values["origin"] = c.origin
	}

	header := http.Header{}
	for name, value := range values {
		if value != "" {
			header[name] = []string{value}
		}
	}
	header[http.HeaderOrderKey] = headerOrder(id.HeaderOrder, header)
	return header
}

// headerOrder follows the preferred order and appends any other header at the end.
func headerOrder(preferred []string, header http.Header) []string {
	if len(preferred) == 0 {
		preferred = identity.DefaultHeaderOrder
	}
	order := make([]string, 0, len(header)+1)
	seen := map[string]bool{}
	for _, name := range preferred {
		name = strings.ToLower(name)
		if !seen[name] {
			order = append(order, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range header {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(order, rest...)
}

// absorbCookies applies Set-Cookie headers to the session. Values are taken
// verbatim from the header, because Go's cookie parser strips the quotes that
// browsers keep (and send back), e.g. in JSESSIONID="ajax:...".
func (c *Client) absorbCookies(resp *http.Response) {
	now := time.Now()
	for _, line := range resp.Header.Values("Set-Cookie") {
		pair, _, _ := strings.Cut(line, ";")
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		deleted := false
		if parsed := (&http.Response{Header: http.Header{"Set-Cookie": {line}}}).Cookies(); len(parsed) == 1 {
			cookie := parsed[0]
			deleted = cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(now))
		}
		if c.session.Update(strings.TrimSpace(name), strings.TrimSpace(value), deleted) {
			slog.Debug("LinkedIn updated a session cookie", "name", name)
			c.changed = true
		}
	}
}

func (c *Client) handle(resp *http.Response, raw []byte, target string) ([]byte, error) {
	status := resp.StatusCode
	location := strings.ToLower(resp.Header.Get("Location"))
	lowered := strings.ToLower(string(raw))
	snippet := strings.Join(strings.Fields(string(raw)), " ")
	if len(snippet) > 300 {
		snippet = snippet[:300] + "…"
	}

	switch {
	case containsAny(location, challengeMarkers):
		return nil, c.challenge("LinkedIn redirected the request to a security checkpoint")
	case containsAny(location, loginMarkers):
		return nil, expired()
	case status == 429 || status == 999:
		reason := fmt.Sprintf("HTTP %d (too many requests)", status)
		if err := c.pacer.StartCooldown(RateLimitCooldown, reason); err != nil {
			slog.Error("could not record the cooldown", "error", err)
		}
		return nil, errs.New(errs.RateLimited, "LinkedIn rate-limited the session: %s. Requests are paused for an hour", reason)
	case status == 401:
		return nil, expired()
	case status == 403 && strings.Contains(lowered, "csrf"):
		return nil, errs.New(errs.AuthRequired, "LinkedIn rejected the session token (CSRF). Run `lmw auth import` again")
	case status == 403 && strings.Contains(lowered, "challenge"):
		return nil, c.challenge("LinkedIn answered with a CHALLENGE")
	case c.session.Validate() != nil:
		return nil, errs.New(errs.AuthRequired, "LinkedIn ended the session. Log in again in your browser and run `lmw auth import`")
	case status >= 300:
		return nil, &errs.Error{Kind: errs.API, Err: &errs.HTTPError{Status: status, URL: target, Snippet: snippet}}
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("{}"), nil
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") || !json.Valid(raw) {
		return nil, errs.New(errs.API, "expected JSON from %s but got %s: %s",
			target, firstNonEmpty(resp.Header.Get("Content-Type"), "something else"), snippet)
	}
	return raw, nil
}

func (c *Client) challenge(reason string) error {
	if err := c.pacer.StartCooldown(ChallengeCooldown, reason); err != nil {
		slog.Error("could not record the cooldown", "error", err)
	}
	return errs.New(errs.Challenge,
		"%s. Requests are paused. Open LinkedIn in your browser, complete the security check, "+
			"then run `lmw auth import` and `lmw limits --clear-cooldown`", reason)
}

func expired() error {
	return errs.New(errs.AuthRequired, "the LinkedIn session has expired. Log in in your browser and run `lmw auth import`")
}

func containsAny(text string, markers []string) bool {
	if text == "" {
		return false
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// IsKind reports whether err is an errs.Error of the given kind.
func IsKind(err error, kind errs.Kind) bool {
	var e *errs.Error
	return errors.As(err, &e) && e.Kind == kind
}
