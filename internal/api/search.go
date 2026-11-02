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
		parameters = append(parameters, "(key:network,value:List("+strings.Join(degrees, "|")+"))")
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
