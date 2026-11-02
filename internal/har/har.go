// Package har reads HAR files exported from a browser's developer tools.
//
// A HAR recorded while browsing LinkedIn shows which internal API calls the
// web app makes (to build or fix commands), the exact headers your browser
// sends (to copy its identity), and, if exported with sensitive data, the
// session cookies (to import the session).
package har

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/normalized"
)

const voyagerPath = "/voyager/api/"

// NameValue is a HAR header, cookie or query parameter.
type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Entry is one request/response pair.
type Entry struct {
	StartedDateTime string `json:"startedDateTime"`
	Request         struct {
		Method   string      `json:"method"`
		URL      string      `json:"url"`
		Headers  []NameValue `json:"headers"`
		Cookies  []NameValue `json:"cookies"`
		PostData *struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int `json:"status"`
		Content struct {
			Size     int    `json:"size"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"content"`
	} `json:"response"`
}

// File is a parsed HAR file.
type File struct {
	Entries []Entry
}

// Load reads a HAR file.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.Wrap(errs.Usage, err, "cannot read %s: %v", path, err)
	}
	var doc struct {
		Log struct {
			Entries []Entry `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errs.Wrap(errs.Usage, err, "%s is not a HAR file: %v", path, err)
	}
	return &File{Entries: doc.Log.Entries}, nil
}

// Call summarizes one Voyager API call.
type Call struct {
	Index    int            `json:"index"`
	Method   string         `json:"method"`
	Path     string         `json:"path"`
	Query    []NameValue    `json:"query,omitempty"`
	Status   int            `json:"status"`
	Size     int            `json:"size"`
	DataKeys []string       `json:"data_keys,omitempty"`
	Types    map[string]int `json:"types,omitempty"`
}

// Calls lists the Voyager API calls whose URL contains filter.
func (f *File) Calls(filter string) []Call {
	var calls []Call
	for i, entry := range f.Entries {
		if !isVoyager(entry) || (filter != "" && !strings.Contains(entry.Request.URL, filter)) {
			continue
		}
		u, err := url.Parse(entry.Request.URL)
		if err != nil {
			continue
		}
		call := Call{Index: i, Method: entry.Request.Method, Path: u.Path, Status: entry.Response.Status, Size: entry.Response.Content.Size}
		for _, part := range strings.Split(u.RawQuery, "&") {
			if part == "" {
				continue
			}
			key, value, _ := strings.Cut(part, "=")
			if decoded, err := url.QueryUnescape(value); err == nil {
				value = decoded
			}
			call.Query = append(call.Query, NameValue{Name: key, Value: value})
		}
		if body, err := entry.ResponseBody(); err == nil && len(body) > 0 {
			call.DataKeys, call.Types = shape(body)
		}
		calls = append(calls, call)
	}
	return calls
}

// ResponseBody returns the decoded response body.
func (e Entry) ResponseBody() ([]byte, error) {
	content := e.Response.Content
	if content.Encoding == "base64" {
		return base64.StdEncoding.DecodeString(content.Text)
	}
	return []byte(content.Text), nil
}

// Header returns a request header value, case-insensitively.
func (e Entry) Header(name string) string {
	for _, h := range e.Request.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// Identity copies the browser identity from the most recent Voyager request.
func (f *File) Identity() (identity.Identity, error) {
	for i := len(f.Entries) - 1; i >= 0; i-- {
		entry := f.Entries[i]
		if !isVoyager(entry) || entry.Header("user-agent") == "" {
			continue
		}
		id := identity.Identity{
			UserAgent:       entry.Header("user-agent"),
			SecCHUA:         entry.Header("sec-ch-ua"),
			SecCHUAMobile:   entry.Header("sec-ch-ua-mobile"),
			SecCHUAPlatform: entry.Header("sec-ch-ua-platform"),
			AcceptLanguage:  entry.Header("accept-language"),
			LiLang:          entry.Header("x-li-lang"),
			LiTrack:         entry.Header("x-li-track"),
			Source:          "har",
		}
		for _, h := range entry.Request.Headers {
			name := strings.ToLower(h.Name)
			if !strings.HasPrefix(name, ":") && !contains(id.HeaderOrder, name) {
				id.HeaderOrder = append(id.HeaderOrder, name)
			}
		}
		if !id.Supported() {
			return identity.Identity{}, errs.New(errs.Usage,
				"the HAR was recorded with a browser lmw cannot imitate (%s); only Chrome-based browsers and Firefox are supported",
				id.UserAgent)
		}
		if id.AcceptLanguage == "" {
			id.AcceptLanguage = "en-US,en;q=0.9"
		}
		if id.LiLang == "" {
			id.LiLang = "en_US"
		}
		return id, nil
	}
	return identity.Identity{}, errs.New(errs.Usage,
		"the HAR file has no LinkedIn API requests with headers. Record it while LinkedIn is loading (see `lmw har --help`)")
}

// Cookies returns the LinkedIn cookies sent with the most recent Voyager request
// that carried a session. They are only present in HARs exported with sensitive data.
func (f *File) Cookies() (map[string]string, error) {
	for i := len(f.Entries) - 1; i >= 0; i-- {
		entry := f.Entries[i]
		if !isVoyager(entry) {
			continue
		}
		cookies := map[string]string{}
		for _, c := range entry.Request.Cookies {
			cookies[c.Name] = c.Value
		}
		if header := entry.Header("cookie"); header != "" {
			for _, part := range strings.Split(header, ";") {
				if name, value, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
					cookies[name] = value
				}
			}
		}
		if cookies["li_at"] != "" {
			return cookies, nil
		}
	}
	return nil, errs.New(errs.Usage,
		"the HAR file has no session cookies. Export it with sensitive data included "+
			"(in Chrome: right-click the request list > \"Save all as HAR (with sensitive data)\"), "+
			"or use `lmw auth import --browser` or the interactive import")
}

// Entry returns the entry at index, as listed by Calls.
func (f *File) Entry(index int) (Entry, error) {
	if index < 0 || index >= len(f.Entries) {
		return Entry{}, errs.New(errs.Usage, "no entry #%d in the HAR file (it has %d entries)", index, len(f.Entries))
	}
	return f.Entries[index], nil
}

func isVoyager(entry Entry) bool {
	u, err := url.Parse(entry.Request.URL)
	return err == nil && strings.HasSuffix(u.Host, "linkedin.com") && strings.HasPrefix(u.Path, voyagerPath)
}

// shape returns the top-level data keys and the counts of included entity types.
func shape(body []byte) ([]string, map[string]int) {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil, nil
	}
	doc := normalized.FromObject(payload)
	var keys []string
	for key := range doc.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	types := map[string]int{}
	for _, entity := range doc.Included {
		t := normalized.String(entity["$type"])
		if t == "" {
			t = "(no $type)"
		}
		types[t[strings.LastIndex(t, ".")+1:]]++
	}
	if len(types) == 0 {
		types = nil
	}
	return keys, types
}

func contains(items []string, item string) bool {
	for _, v := range items {
		if v == item {
			return true
		}
	}
	return false
}

// String renders a call on one line for listings.
func (c Call) String() string {
	line := fmt.Sprintf("#%-4d %-4s %d %s", c.Index, c.Method, c.Status, c.Path)
	if len(c.Query) > 0 {
		var parts []string
		for _, q := range c.Query {
			value := q.Value
			if len(value) > 60 {
				value = value[:60] + "…"
			}
			parts = append(parts, q.Name+"="+value)
		}
		line += "?" + strings.Join(parts, "&")
	}
	if len(c.Types) > 0 {
		var types []string
		for t, n := range c.Types {
			types = append(types, fmt.Sprintf("%s×%d", t, n))
		}
		sort.Strings(types)
		line += "\n      included: " + strings.Join(types, ", ")
	}
	return line
}

// QueryUse is a GraphQL query the web app ran: the queryId name and hash,
// and the names of the variables it sent.
type QueryUse struct {
	Name      string
	Hash      string
	Variables []string
}

// Queries lists the GraphQL queries in the file, most recent first, each
// name and set of variables once.
func (f *File) Queries() []QueryUse {
	var uses []QueryUse
	seen := map[string]bool{}
	for i := len(f.Entries) - 1; i >= 0; i-- {
		entry := f.Entries[i]
		if !isVoyager(entry) || entry.Response.Status >= 400 {
			continue
		}
		u, err := url.Parse(entry.Request.URL)
		if err != nil {
			continue
		}
		name, hash, ok := strings.Cut(u.Query().Get("queryId"), ".")
		if !ok || name == "" || hash == "" {
			continue
		}
		var variables []string
		if entry.Request.Method == "POST" && entry.Request.PostData != nil {
			var body struct {
				Variables map[string]any `json:"variables"`
			}
			if json.Unmarshal([]byte(entry.Request.PostData.Text), &body) == nil {
				for key := range body.Variables {
					variables = append(variables, key)
				}
			}
		} else {
			variables = restliKeys(rawQueryValue(u.RawQuery, "variables"))
		}
		sort.Strings(variables)
		key := name + "(" + strings.Join(variables, ",") + ")"
		if seen[key] {
			continue
		}
		seen[key] = true
		uses = append(uses, QueryUse{Name: name, Hash: hash, Variables: variables})
	}
	return uses
}

// rawQueryValue returns a query parameter decoded once, without the "+" as
// space rule that would corrupt Rest.li values.
func rawQueryValue(rawQuery, key string) string {
	for _, part := range strings.Split(rawQuery, "&") {
		if k, v, ok := strings.Cut(part, "="); ok && k == key {
			if decoded, err := url.PathUnescape(v); err == nil {
				return decoded
			}
			return v
		}
	}
	return ""
}

// restliKeys returns the top-level keys of a Rest.li record such as
// "(count:10,query:(keywords:x),start:0)".
func restliKeys(record string) []string {
	record = strings.TrimSpace(record)
	if !strings.HasPrefix(record, "(") || !strings.HasSuffix(record, ")") {
		return nil
	}
	record = record[1 : len(record)-1]
	var keys []string
	depth, start := 0, 0
	for i := 0; i <= len(record); i++ {
		if i < len(record) {
			switch record[i] {
			case '(':
				depth++
				continue
			case ')':
				depth--
				continue
			case ',':
				if depth > 0 {
					continue
				}
			default:
				continue
			}
		}
		if key, _, ok := strings.Cut(record[start:i], ":"); ok && key != "" {
			keys = append(keys, strings.TrimSpace(key))
		}
		start = i + 1
	}
	return keys
}
