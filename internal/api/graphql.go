package api

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

// Many pages of the web app read through GraphQL queries named by a
// queryId such as voyagerSearchDashClusters.b0928897b71bd00a5a7291755dcd64f0.
// The hash after the dot changes when LinkedIn deploys a new version of a
// page, and LinkedIn answers HTTP 400 to hashes it no longer knows, so each
// query keeps a list of known hashes and tries them in order. Hashes learned
// from a HAR file (`lmw har learn`) are tried first.

// Query is a GraphQL query of the web app.
type Query struct {
	// Name is the part of the queryId before the dot.
	Name string
	// Vars are the variables lmw sends; Optional ones may also appear in the
	// web app's calls. Queries that share a name are told apart by them.
	Vars, Optional []string
	// Hashes are the known hashes, the most recently verified first.
	Hashes []string
	// Messaging queries live under their own path and take no
	// includeWebMetadata parameter.
	Messaging bool
}

// LearnedQuery is a query seen in a HAR file: its name, the variables the
// web app sent and the hash it used.
type LearnedQuery struct {
	Name      string   `json:"name"`
	Variables []string `json:"variables"`
	Hash      string   `json:"hash"`
}

var (
	learnedMu sync.RWMutex
	learned   []LearnedQuery
)

// SetLearnedQueries makes matching queries try these hashes first.
func SetLearnedQueries(queries []LearnedQuery) {
	learnedMu.Lock()
	defer learnedMu.Unlock()
	learned = append([]LearnedQuery(nil), queries...)
}

// Matches reports whether a query seen in a HAR file is this query.
func (q Query) Matches(l LearnedQuery) bool {
	if l.Name != q.Name || l.Hash == "" {
		return false
	}
	seen := map[string]bool{}
	for _, v := range l.Variables {
		seen[v] = true
	}
	allowed := map[string]bool{}
	for _, v := range q.Vars {
		if !seen[v] {
			return false
		}
		allowed[v] = true
	}
	for _, v := range q.Optional {
		allowed[v] = true
	}
	for _, v := range l.Variables {
		if !allowed[v] {
			return false
		}
	}
	return true
}

// candidates returns the hashes to try, learned ones first and without repeats.
func (q Query) candidates() []string {
	var hashes []string
	learnedMu.RLock()
	for i := len(learned) - 1; i >= 0; i-- {
		if q.Matches(learned[i]) {
			hashes = append(hashes, learned[i].Hash)
		}
	}
	learnedMu.RUnlock()
	var out []string
	seen := map[string]bool{}
	for _, h := range append(hashes, q.Hashes...) {
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

func (q Query) path() string {
	if q.Messaging {
		return "/voyagerMessagingGraphQL/graphql"
	}
	return "/graphql"
}

// Get runs the query with variables in Rest.li syntax, such as
// "(count:10,start:0)", and returns the raw normalized response.
func (q Query) Get(ctx context.Context, c Client, variables string) ([]byte, error) {
	var lastErr error
	for _, hash := range q.candidates() {
		id := q.Name + "." + hash
		var params voyager.Params
		if q.Messaging {
			params = voyager.Params{{Key: "queryId", Value: id}, {Key: "variables", Value: variables}}
		} else {
			params = voyager.Params{{Key: "includeWebMetadata", Value: "true"}, {Key: "variables", Value: variables}, {Key: "queryId", Value: id}}
		}
		raw, err := c.Get(ctx, q.path(), params)
		if !endpointGone(err) {
			return raw, err
		}
		lastErr = err
	}
	return nil, q.stale(lastErr)
}

// Execute runs a GraphQL mutation (such as a reaction) with JSON variables.
func (q Query) Execute(ctx context.Context, c Client, variables map[string]any) ([]byte, error) {
	var lastErr error
	for _, hash := range q.candidates() {
		id := q.Name + "." + hash
		raw, err := c.Post(ctx, q.path(), voyager.Params{{Key: "action", Value: "execute"}, {Key: "queryId", Value: id}},
			map[string]any{"variables": variables, "queryId": id, "includeWebMetadata": true})
		if !endpointGone(err) {
			return raw, err
		}
		lastErr = err
	}
	return nil, q.stale(lastErr)
}

func (q Query) stale(err error) error {
	return errs.Wrap(errs.API, err, "LinkedIn no longer accepts the versions of its %s query that lmw knows (%v). "+
		"Record a HAR while the matching LinkedIn page loads, then run `lmw har learn FILE`", q.Name, err)
}

// Rest.li structure characters inside values must be percent-encoded, or
// LinkedIn reads them as structure and answers 400.
func restliValue(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// collection finds the elements of a collection in a normalized response:
// data["*elements"] for REST endpoints, or data.data.<key>["*elements"]
// (or "elements") for GraphQL ones. It also returns the collection object
// for its paging metadata.
func collection(doc *normalized.Doc) ([]normalized.Object, int, normalized.Object) {
	if items, n := elementsOf(doc, doc.Data); n > 0 || doc.Data["*elements"] != nil || doc.Data["elements"] != nil {
		return items, n, doc.Data
	}
	inner, _ := doc.Data["data"].(normalized.Object)
	if inner == nil {
		inner = doc.Data
	}
	keys := make([]string, 0, len(inner))
	for key := range inner {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var coll normalized.Object
		switch v := inner[key].(type) {
		case normalized.Object:
			coll = v
		case string:
			if strings.HasPrefix(key, "*") {
				coll = doc.Get(v)
			}
		}
		if coll == nil || (coll["*elements"] == nil && coll["elements"] == nil) {
			continue
		}
		items, n := elementsOf(doc, coll)
		return items, n, coll
	}
	return nil, 0, nil
}

// elementsOf resolves the elements of one collection object, in order.
func elementsOf(doc *normalized.Doc, coll normalized.Object) ([]normalized.Object, int) {
	for _, key := range []string{"*elements", "elements"} {
		items, ok := coll[key].([]any)
		if !ok {
			continue
		}
		var out []normalized.Object
		for _, item := range items {
			switch v := item.(type) {
			case normalized.Object:
				out = append(out, v)
			case string:
				if entity := doc.Get(v); entity != nil {
					out = append(out, entity)
				}
			}
		}
		return out, len(items)
	}
	return nil, 0
}

// nextStart reads the start of the next page from a collection's paging
// metadata, or returns start+n when there is none.
func nextStart(coll normalized.Object, start, n int) int {
	for _, path := range [][]string{{"metadata", "nextStart"}, {"paging", "nextStart"}} {
		if next, ok := normalized.Int(normalized.Dig(coll, path...)); ok && next > start {
			return next
		}
	}
	return start + n
}
