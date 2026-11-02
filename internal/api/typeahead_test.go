package api

import (
	"context"
	"strings"
	"testing"
)

func TestPeopleSearchGoesThroughTheTypeahead(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/graphql": doc(t, map[string]any{"data": map[string]any{"messagingDashMessagingTypeaheadByTypeaheadKeyword": map[string]any{
			"elements": []any{
				map[string]any{"result": map[string]any{"*profile": "urn:li:fsd_profile:ACoAAG"}},
				map[string]any{"result": map[string]any{"*profile": "urn:li:fsd_profile:ACoAAH"}},
			}}}},
			map[string]any{"$type": "com.linkedin.voyager.dash.identity.profile.Profile", "entityUrn": "urn:li:fsd_profile:ACoAAG",
				"firstName": "Grace", "lastName": "Hopper", "headline": "Admiral"},
			map[string]any{"$type": "com.linkedin.voyager.dash.identity.profile.Profile", "entityUrn": "urn:li:fsd_profile:ACoAAH",
				"firstName": "Alan", "lastName": "Turing"},
		),
	}}
	results, err := Search(context.Background(), r, "people", "grace hopper", SearchOptions{}, 1)
	if err != nil || len(results) != 1 {
		t.Fatalf("results %+v, %v", results, err)
	}
	if g := results[0]; g.Name != "Grace Hopper" || g.Subtitle != "Admiral" || g.URL != "https://www.linkedin.com/in/ACoAAG/" || g.URN != "urn:li:fsd_profile:ACoAAG" {
		t.Errorf("result %+v", g)
	}
	call := r.last(t)
	if !strings.HasPrefix(call.param("queryId"), "voyagerMessagingDashMessagingTypeahead.") ||
		call.param("variables") != "(keyword:grace%20hopper,types:List(CONNECTIONS,PEOPLE,COWORKERS))" {
		t.Errorf("typeahead request %+v", call.params)
	}

	if _, err := Search(context.Background(), r, "people", "grace", SearchOptions{Network: []string{"1st"}}, 5); err != nil ||
		r.last(t).param("variables") != "(keyword:grace,types:List(CONNECTIONS))" {
		t.Errorf("1st-degree typeahead: %v %+v", err, r.last(t).params)
	}
	r.calls = nil
	if _, err := Search(context.Background(), r, "people", "grace", SearchOptions{Network: []string{"2nd"}}, 5); err != nil ||
		!strings.HasPrefix(r.last(t).param("queryId"), "voyagerSearchDashClusters.") {
		t.Errorf("2nd degree must use the search query: %v %+v", err, r.calls)
	}
}
