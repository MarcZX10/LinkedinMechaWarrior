package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
)

// SearchTypes maps what can be searched to LinkedIn's result types. Posts
// are only searchable through a server-driven page lmw does not read.
var SearchTypes = map[string]string{
	"people":    "PEOPLE",
	"companies": "COMPANIES",
	"schools":   "SCHOOLS",
	"groups":    "GROUPS",
}

// SearchBoxCaveat explains empty answers of the search box query, which since
// November 2026 has answered every search with no results for some accounts.
const SearchBoxCaveat = "LinkedIn's search box query has answered every search with no results since November 2026, " +
	"so an empty answer may not mean there is nothing; `lmw doctor --probe` checks it"

// NetworkDegrees maps connection degrees to LinkedIn's search filter values.
var NetworkDegrees = map[string]string{"1st": "F", "2nd": "S", "3rd": "O"}

// SearchResult is one person, company, school or group found by a search.
type SearchResult struct {
	Name     string `json:"name"`
	Subtitle string `json:"subtitle,omitempty"`
	Location string `json:"location,omitempty"`
	Summary  string `json:"summary,omitempty"`
	URL      string `json:"url,omitempty"`
	URN      string `json:"urn,omitempty"`
}

// SearchOptions narrow a search.
type SearchOptions struct {
	// Network keeps people of these degrees: "1st", "2nd", "3rd".
	Network []string
}

// Search runs a search like the website's search box, one page at a time.
func Search(ctx context.Context, c Client, kind, keywords string, opts SearchOptions, limit int) ([]SearchResult, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	resultType, ok := SearchTypes[kind]
	if !ok {
		return nil, errs.New(errs.Usage, "cannot search %q; use people, companies, schools or groups", kind)
	}
	keywords = strings.Join(strings.Fields(keywords), " ")
	if kind == "people" && keywords != "" {
		if types := typeaheadTypes(opts.Network); types != nil {
			results, err := searchPeopleByName(ctx, c, keywords, types, limit)
			if !endpointGone(err) && !IsStale(err) {
				return results, err
			}
		}
	}
	parameters := []string{"(key:resultType,value:List(" + resultType + "))"}
	if len(opts.Network) > 0 {
		if kind != "people" {
			return nil, errs.New(errs.Usage, "the network filter only applies to people")
		}
		var degrees []string
		for _, d := range opts.Network {
			value, ok := NetworkDegrees[d]
			if !ok {
				return nil, errs.New(errs.Usage, "unknown network degree %q; use 1st, 2nd or 3rd", d)
			}
			degrees = append(degrees, value)
		}
		// A Rest.li list: List(F,S), not one "F|S" value.
		parameters = append(parameters, "(key:network,value:List("+strings.Join(degrees, ",")+"))")
	}
	if keywords == "" && len(parameters) == 1 {
		return nil, errs.New(errs.Usage, "give some keywords to search for")
	}
	query := "flagshipSearchIntent:SEARCH_SRP,queryParameters:List(" + strings.Join(parameters, ",") + "),includeFiltersInResponse:false"
	// LinkedIn answers 400 to empty keywords; leave them out instead.
	if keywords != "" {
		query = "keywords:" + restliValue(keywords) + "," + query
	}
	origin := "GLOBAL_SEARCH_HEADER"
	if len(parameters) > 1 {
		origin = "FACETED_SEARCH"
	}
	return paginate(limit, func(start int) ([]SearchResult, int, error) {
		raw, err := searchQuery.Get(ctx, c, fmt.Sprintf("(start:%d,origin:%s,query:(%s))", start, origin, query))
		if err != nil {
			return nil, 0, err
		}
		found, n := ParseSearch(raw)
		return found, pageEnd(nil, start, n, ListPageSize), nil
	})
}

// typeaheadTypes picks the typeahead matches for a network filter, or nil
// when the typeahead cannot express it (2nd and 3rd degree).
func typeaheadTypes(network []string) []string {
	if len(network) == 0 {
		return []string{"CONNECTIONS", "PEOPLE", "COWORKERS"}
	}
	if len(network) == 1 && network[0] == "1st" {
		return []string{"CONNECTIONS"}
	}
	return nil
}

// ParseSearch reads one page of search results and how many items it had.
func ParseSearch(raw []byte) ([]SearchResult, int) {
	rows, items := searchRows(raw)
	var out []SearchResult
	for _, r := range rows {
		name := normalized.Text(r["title"])
		if name == "" || strings.EqualFold(name, "LinkedIn Member") {
			continue
		}
		out = append(out, SearchResult{
			Name:     name,
			Subtitle: normalized.Text(r["primarySubtitle"]),
			Location: normalized.Text(r["secondarySubtitle"]),
			Summary:  normalized.Text(r["summary"]),
			URL:      cleanURL(normalized.String(r["navigationUrl"])),
			URN:      firstNonEmpty(normalized.String(r["trackingUrn"]), normalized.String(r["entityUrn"])),
		})
	}
	return out, items
}

// searchRows returns the result rows of a search page, in order, and how many
// items the page had.
func searchRows(raw []byte) ([]normalized.Object, int) {
	doc := normalized.Parse(raw)
	var results []normalized.Object
	items := 0
	clusters, _, coll := collection(doc)
	for _, cluster := range clusters {
		for _, item := range listOfObjects(cluster["items"]) {
			items++
			inner, _ := normalized.Dig(item, "item").(normalized.Object)
			if result := doc.Ref(inner, "entityResult"); result != nil {
				results = append(results, result)
			}
		}
	}
	if coll == nil {
		results = doc.OfType("EntityResultViewModel")
		items = len(results)
	}
	return results, items
}

func listOfObjects(value any) []normalized.Object {
	list, _ := value.([]any)
	var out []normalized.Object
	for _, item := range list {
		if obj, ok := item.(normalized.Object); ok {
			out = append(out, obj)
		}
	}
	return out
}

// peopleTypeaheadQuery is the person lookup of the message composer. Since
// autumn 2026 the search box query answers name searches with no results,
// while this one still finds people by name (verified live on 2026-11-02).
// It returns up to about ten people, without paging.
var peopleTypeaheadQuery = Query{
	Name: "voyagerMessagingDashMessagingTypeahead", Vars: []string{"keyword", "types"},
	Hashes: []string{"7f566173ac0c94b510b3dc2b2a6763d4"},
}

// searchPeopleByName finds people through the composer's typeahead. types
// are its kinds of match: CONNECTIONS, PEOPLE, COWORKERS.
func searchPeopleByName(ctx context.Context, c Client, keywords string, types []string, limit int) ([]SearchResult, error) {
	raw, err := peopleTypeaheadQuery.Get(ctx, c, "(keyword:"+restliValue(keywords)+",types:List("+strings.Join(types, ",")+"))")
	if err != nil {
		return nil, err
	}
	results := ParseTypeahead(raw)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// ParseTypeahead reads the people of a typeahead answer, in its order.
func ParseTypeahead(raw []byte) []SearchResult {
	doc := normalized.Parse(raw)
	var people []normalized.Object
	items, _, _ := collection(doc)
	for _, item := range items {
		if person := profileIn(doc, item); person != nil {
			people = append(people, person)
		}
	}
	if len(people) == 0 {
		people = doc.OfType(".Profile")
	}
	var out []SearchResult
	seen := map[string]bool{}
	for _, p := range people {
		name, headline, url, urn := personSummary(p)
		if name == "" || seen[urn] {
			continue
		}
		seen[urn] = true
		if url == "" {
			if id := memberIDFromURN(urn); id != "" {
				url = "https://www.linkedin.com/in/" + id + "/"
			}
		}
		out = append(out, SearchResult{Name: name, Subtitle: headline, URL: url, URN: urn})
	}
	return out
}

// profileIn finds the profile a typeahead element is about: the element
// itself, or a profile it references, directly or one object down.
func profileIn(doc *normalized.Doc, item normalized.Object) normalized.Object {
	isProfile := func(e normalized.Object) bool {
		return e != nil && strings.HasSuffix(normalized.String(e["$type"]), ".Profile") && normalized.String(e["firstName"]) != ""
	}
	if isProfile(item) {
		return item
	}
	for key, value := range item {
		switch v := value.(type) {
		case string:
			if strings.HasPrefix(key, "*") && isProfile(doc.Get(v)) {
				return doc.Get(v)
			}
		case normalized.Object:
			for innerKey, inner := range v {
				if s, ok := inner.(string); ok && strings.HasPrefix(innerKey, "*") && isProfile(doc.Get(s)) {
					return doc.Get(s)
				}
			}
		}
	}
	return nil
}
