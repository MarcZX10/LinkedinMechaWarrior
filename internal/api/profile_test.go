package api

import (
	"context"
	"strings"
	"testing"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

func TestParseProfileViewClassicShape(t *testing.T) {
	raw := doc(t, map[string]any{"*profile": "urn:li:fs_profile:ACoAAB"},
		map[string]any{
			"$type": "com.linkedin.voyager.identity.profile.Profile", "entityUrn": "urn:li:fs_profile:ACoAAB",
			"firstName": "Ada", "lastName": "Lovelace", "headline": "Analyst", "summary": "I like engines.",
			"locationName": "London", "industryName": "Research",
		},
		map[string]any{"$type": "com.linkedin.voyager.identity.shared.MiniProfile", "firstName": "Someone", "lastName": "Else", "publicIdentifier": "else"},
		map[string]any{
			"$type": "com.linkedin.voyager.identity.profile.Position", "title": "Analyst", "companyName": "Analytical Engines",
			"timePeriod": map[string]any{"startDate": map[string]any{"year": 1842, "month": 7}},
		},
		map[string]any{
			"$type": "com.linkedin.voyager.identity.profile.Education", "schoolName": "Home", "fieldOfStudy": "Mathematics",
			"timePeriod": map[string]any{"startDate": map[string]any{"year": 1830}, "endDate": map[string]any{"year": 1835}},
		},
		map[string]any{"$type": "com.linkedin.voyager.identity.profile.Skill", "name": "Algorithms"},
		map[string]any{"$type": "com.linkedin.voyager.identity.profile.Skill", "name": "Algorithms"},
	)
	p, err := ParseProfileView([]byte(raw), ProfileRef{PublicID: "ada-lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Ada Lovelace" || p.Headline != "Analyst" || p.About != "I like engines." || p.Location != "London" ||
		p.URL != "https://www.linkedin.com/in/ada-lovelace/" || p.URN != "urn:li:fsd_profile:ACoAAB" {
		t.Errorf("profile %+v", p.Profile)
	}
	if len(p.Experience) != 1 || p.Experience[0].Company != "Analytical Engines" || p.Experience[0].Start != "1842-07" {
		t.Errorf("experience %+v", p.Experience)
	}
	if len(p.Education) != 1 || p.Education[0].End != "1835" || len(p.Skills) != 1 {
		t.Errorf("education %+v, skills %v", p.Education, p.Skills)
	}
}

func TestParseProfileViewDashShape(t *testing.T) {
	raw := doc(t, map[string]any{"*elements": []any{"urn:li:fsd_profile:ACoAAB"}},
		map[string]any{
			"$type": "com.linkedin.voyager.dash.identity.profile.Profile", "entityUrn": "urn:li:fsd_profile:ACoAAB",
			"firstName": "Ada", "lastName": "Lovelace", "headline": "Analyst", "publicIdentifier": "ada-lovelace",
			"geoLocation": map[string]any{"*geo": "urn:li:fsd_geo:1"},
		},
		map[string]any{"$type": "com.linkedin.voyager.dash.common.Geo", "entityUrn": "urn:li:fsd_geo:1", "defaultLocalizedName": "London, England"},
		map[string]any{
			"$type": "com.linkedin.voyager.dash.identity.profile.Position", "title": "Analyst", "companyName": "Analytical Engines",
			"dateRange": map[string]any{"start": map[string]any{"year": 1842}, "end": map[string]any{"year": 1843, "month": 9}},
		},
	)
	p, err := ParseProfileView([]byte(raw), ProfileRef{MemberID: "ACoAAB"})
	if err != nil {
		t.Fatal(err)
	}
	if p.PublicID != "ada-lovelace" || p.Location != "London, England" || p.URN != "urn:li:fsd_profile:ACoAAB" {
		t.Errorf("profile %+v", p)
	}
	if p.Experience[0].End != "1843-09" {
		t.Errorf("experience %+v", p.Experience)
	}
	if _, err := ParseProfileView([]byte(`{"data":{},"included":[]}`), ProfileRef{PublicID: "x"}); err == nil ||
		!strings.Contains(err.Error(), "no profile found") {
		t.Errorf("expected an error for an empty answer, got %v", err)
	}
}

func TestParseContactInfo(t *testing.T) {
	// Cut down from the overlay stream: a label, then the field's view and value.
	label := func(text string) string {
		return `["$","p",null,{"className":"t-16 t-black t-bold","children":["` + text + `"]}]`
	}
	stream := `0:{"x":1}` + "\n" + `1:["$","div",null,{"viewName":"profile-contact-info-details-view"}]` +
		`{"viewName":"contact-email"}` + label("Email") +
		`{"urlValue":{"$case":"url","url":"mailto:ada@example.com"},"children":["ada@example.com"]}` +
		`{"viewName":"contact-website"}` + label("Website") +
		`{"urlValue":{"$case":"url","url":"https://www.linkedin.com/safety/go?url=https%3A%2F%2Fada.example&trk=x"},"children":["ada.example"]}` +
		`{"viewName":"contact-birthday"}` + label("Birthday") + `{"children":["December 10"]}` +
		label("Connected since") + `{"children":["1 Jan 2020"]}` +
		`{"viewName":"premium-upsell-card"}` + label("Try Premium") + `{"children":["upsell"]}`
	info, err := ParseContactInfo([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "ada@example.com" || len(info.Websites) != 1 || info.Websites[0] != "https://ada.example" ||
		info.Birthday != "December 10" || info.ConnectedSince != "1 Jan 2020" || info.Other != nil {
		t.Errorf("contact info %+v", info)
	}
	if _, err := ParseContactInfo([]byte(`<html>`)); err == nil {
		t.Error("expected an error for an unknown answer")
	}
}
func TestGetProfileTriesTheShorterDecoration(t *testing.T) {
	r := &routes{replies: map[string]string{
		"/identity/dash/profiles": doc(t, map[string]any{"*elements": []any{"urn:li:fsd_profile:ACoAAB"}},
			map[string]any{"$type": "com.linkedin.voyager.dash.identity.profile.Profile", "entityUrn": "urn:li:fsd_profile:ACoAAB", "firstName": "Ada"}),
	}}
	// The first decoration fails like a retired one would.
	gone := &goneFirst{routes: r}
	p, err := GetProfile(context.Background(), gone, ProfileRef{PublicID: "ada-lovelace"})
	if err != nil || p.FirstName != "Ada" {
		t.Fatalf("profile %+v, %v", p, err)
	}
	if len(r.calls) != 2 || r.calls[0].param("decorationId") != ProfileDecoration || r.calls[1].param("decorationId") != ProfileShortDecoration ||
		r.calls[1].param("memberIdentity") != "ada-lovelace" {
		t.Errorf("calls %+v", r.calls)
	}
}
func TestProfilePostsLooksUpTheMember(t *testing.T) {
	update := map[string]any{
		"$type": "com.linkedin.voyager.dash.feed.Update", "entityUrn": "urn:li:fsd_update:(urn:li:activity:42,MEMBER_SHARES,EMPTY,DEFAULT,false)",
		"actor":      map[string]any{"name": map[string]any{"text": "Ada Lovelace"}},
		"commentary": map[string]any{"text": map[string]any{"text": "Engines!"}},
	}
	r := &routes{replies: map[string]string{
		"/identity/dash/profiles": doc(t, map[string]any{"*elements": []any{"urn:li:fsd_profile:ACoAAB"}},
			map[string]any{"$type": "com.linkedin.voyager.dash.identity.profile.Profile", "entityUrn": "urn:li:fsd_profile:ACoAAB", "firstName": "Ada", "publicIdentifier": "ada"}),
		"/graphql": doc(t, map[string]any{"data": map[string]any{"feedDashProfileUpdatesByMemberShareFeed": map[string]any{
			"*elements": []any{update["entityUrn"]}}}}, update),
	}}
	posts, err := ProfilePosts(context.Background(), r, ProfileRef{PublicID: "ada"}, 1)
	if err != nil || len(posts) != 1 || posts[0].Text != "Engines!" || posts[0].URN != "urn:li:activity:42" {
		t.Fatalf("posts %+v, %v", posts, err)
	}
	vars := r.calls[1].param("variables")
	if !strings.Contains(r.calls[1].param("queryId"), "voyagerFeedDashProfileUpdates.") ||
		!strings.Contains(vars, "profileUrn:urn%3Ali%3Afsd_profile%3AACoAAB") {
		t.Errorf("profile updates asked with %+v", r.calls[1].params)
	}

	r.calls = nil
	if _, err := ProfilePosts(context.Background(), r, ProfileRef{MemberID: "ACoAAC"}, 1); err != nil || len(r.calls) != 1 {
		t.Errorf("a member id needs no lookup: %v, %+v", err, r.calls)
	}
}

// goneFirst answers the first request with HTTP 404, like a removed endpoint.
type goneFirst struct {
	*routes
	failed bool
}

func (g *goneFirst) Get(ctx context.Context, path string, params voyager.Params) ([]byte, error) {
	if !g.failed {
		g.failed = true
		g.calls = append(g.calls, call{method: "GET", path: path, params: params})
		return nil, &errs.Error{Kind: errs.API, Err: &errs.HTTPError{Status: 404, URL: path}}
	}
	return g.routes.Get(ctx, path, params)
}
