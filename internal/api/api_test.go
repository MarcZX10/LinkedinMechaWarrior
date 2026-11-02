package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

var meIncluded = []any{map[string]any{
	"$type":            "com.linkedin.voyager.identity.shared.MiniProfile",
	"entityUrn":        "urn:li:fs_miniProfile:ACoAAB",
	"dashEntityUrn":    "urn:li:fsd_profile:ACoAAB",
	"firstName":        "Ada",
	"lastName":         "Lovelace",
	"occupation":       "Analyst at Analytical Engines",
	"publicIdentifier": "ada-lovelace",
}}

func TestParseMe(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"data":     map[string]any{"plainId": 123, "*miniProfile": "urn:li:fs_miniProfile:ACoAAB"},
		"included": meIncluded,
	})
	p, err := ParseMe(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Ada Lovelace" || p.Headline != "Analyst at Analytical Engines" ||
		p.URL != "https://www.linkedin.com/in/ada-lovelace/" || p.URN != "urn:li:fsd_profile:ACoAAB" {
		t.Errorf("unexpected profile %+v", p)
	}

	fallback, err := ParseMe(mustJSON(t, map[string]any{"data": map[string]any{}, "included": meIncluded}))
	if err != nil || fallback.FirstName != "Ada" {
		t.Errorf("fallback to included profile failed: %+v %v", fallback, err)
	}

	if _, err := ParseMe([]byte(`{"data":{},"included":[]}`)); err == nil {
		t.Error("expected an error for an unknown shape")
	}
}

type update map[string]any

func feedUpdate(id int, author, text string) update {
	u := update{
		"$type":          "com.linkedin.voyager.feed.render.UpdateV2",
		"entityUrn":      fmt.Sprintf("urn:li:fs_updateV2:(urn:li:activity:%d,MAIN_FEED,EMPTY,DEFAULT,false)", id),
		"updateMetadata": map[string]any{"urn": fmt.Sprintf("urn:li:activity:%d", id)},
		"actor": map[string]any{
			"name":           map[string]any{"text": author, "attributes": []any{}},
			"description":    map[string]any{"text": "Engineer"},
			"subDescription": map[string]any{"text": "3h • Edited • "},
		},
		"*socialDetail": fmt.Sprintf("urn:li:fs_socialDetail:urn:li:activity:%d", id),
	}
	if text != "" {
		u["commentary"] = map[string]any{"text": map[string]any{"text": text}}
	}
	return u
}

func feedPage(t *testing.T, updates []update, extra ...any) []byte {
	t.Helper()
	var included, elements []any
	for _, u := range updates {
		activity := u["updateMetadata"].(map[string]any)["urn"]
		elements = append(elements, u["entityUrn"])
		included = append(included, map[string]any(u),
			map[string]any{
				"entityUrn":                  fmt.Sprintf("urn:li:fs_socialDetail:%s", activity),
				"*totalSocialActivityCounts": fmt.Sprintf("urn:li:fs_socialActivityCounts:%s", activity),
			},
			map[string]any{
				"entityUrn":   fmt.Sprintf("urn:li:fs_socialActivityCounts:%s", activity),
				"numLikes":    12,
				"numComments": 3,
				"numShares":   1,
			})
	}
	included = append(included, extra...)
	return mustJSON(t, map[string]any{"data": map[string]any{"*elements": elements}, "included": included})
}

func TestParseFeedExtractsPostsAndCounts(t *testing.T) {
	posts, count := ParseFeed(feedPage(t, []update{feedUpdate(1, "Grace Hopper", "Hello\nworld")}))
	if count != 1 || len(posts) != 1 {
		t.Fatalf("got %d posts from %d elements", len(posts), count)
	}
	p := posts[0]
	if p.Author != "Grace Hopper" || p.AuthorHeadline != "Engineer" || p.Age != "3h" || p.Text != "Hello\nworld" {
		t.Errorf("unexpected post %+v", p)
	}
	if *p.Reactions != 12 || *p.Comments != 3 || *p.Reposts != 1 {
		t.Errorf("unexpected counts %d %d %d", *p.Reactions, *p.Comments, *p.Reposts)
	}
	if p.URL != "https://www.linkedin.com/feed/update/urn:li:activity:1/" || p.Promoted {
		t.Errorf("unexpected post %+v", p)
	}
}

func TestParseFeedHandlesResharesAndPromotedPosts(t *testing.T) {
	original := feedUpdate(2, "Alan Turing", "Original thoughts")
	original["entityUrn"] = "urn:li:fs_updateV2:original"
	repost := feedUpdate(3, "Grace Hopper", "")
	repost["*resharedUpdate"] = "urn:li:fs_updateV2:original"
	promoted := feedUpdate(4, "Some Brand", "Buy things")
	promoted["actor"].(map[string]any)["subDescription"] = map[string]any{"text": "Promoted"}

	posts, count := ParseFeed(feedPage(t, []update{repost, promoted}, map[string]any(original)))
	if count != 2 || len(posts) != 2 {
		t.Fatalf("got %d posts from %d elements", len(posts), count)
	}
	if posts[0].ResharedAuthor != "Alan Turing" || posts[0].Text != "Original thoughts" {
		t.Errorf("unexpected repost %+v", posts[0])
	}
	if !posts[1].Promoted || posts[1].Age != "" {
		t.Errorf("promoted post not flagged, or label kept as its age: %+v", posts[1])
	}
}

func TestParseFeedToleratesUnknownData(t *testing.T) {
	posts, count := ParseFeed([]byte(`{"data":{"*elements":["urn:li:missing","urn:li:empty"]},"included":[{"entityUrn":"urn:li:empty"}]}`))
	if len(posts) != 0 || count != 2 {
		t.Errorf("got %d posts from %d elements", len(posts), count)
	}
	for _, garbage := range []string{"", "null", `{"data":"x","included":"y"}`, "[1,2]"} {
		if posts, count := ParseFeed([]byte(garbage)); len(posts) != 0 || count != 0 {
			t.Errorf("%q: got %d posts from %d elements", garbage, len(posts), count)
		}
	}
}

type fakeClient struct {
	pages    [][]byte
	calls    []voyager.Params
	posted   []any
	response []byte
}

func (f *fakeClient) Get(_ context.Context, _ string, params voyager.Params) ([]byte, error) {
	f.calls = append(f.calls, params)
	if len(f.pages) == 0 {
		return []byte(`{"data":{"*elements":[]},"included":[]}`), nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

func (f *fakeClient) Post(_ context.Context, _ string, _ voyager.Params, payload any) ([]byte, error) {
	f.posted = append(f.posted, payload)
	return f.response, nil
}

func (f *fakeClient) Delete(context.Context, string, voyager.Params) ([]byte, error) {
	return f.response, nil
}

func (f *fakeClient) PostSDUI(context.Context, string, voyager.Params, any) ([]byte, error) {
	return f.response, nil
}

func pageOf(t *testing.T, from, to int) []byte {
	var updates []update
	for i := from; i < to; i++ {
		updates = append(updates, feedUpdate(i, fmt.Sprintf("Author %d", i), fmt.Sprintf("Post %d", i)))
	}
	return feedPage(t, updates)
}

func TestFeedPaginatesUntilLimit(t *testing.T) {
	c := &fakeClient{pages: [][]byte{pageOf(t, 0, 10), pageOf(t, 10, 20)}}
	posts, err := Feed(context.Background(), c, 15)
	if err != nil || len(posts) != 15 {
		t.Fatalf("got %d posts, %v", len(posts), err)
	}
	if len(c.calls) != 2 || c.calls[0][2].Value != "0" || c.calls[1][2].Value != "10" {
		t.Errorf("unexpected pagination %v", c.calls)
	}
}

func TestFeedStopsOnEmptyPageAndCapsPages(t *testing.T) {
	c := &fakeClient{pages: [][]byte{pageOf(t, 1, 2)}}
	if posts, _ := Feed(context.Background(), c, 20); len(posts) != 1 {
		t.Errorf("got %d posts", len(posts))
	}

	unparseable := []byte(`{"data":{"*elements":["a","b","c","d","e","f","g","h","i","j"]},"included":[]}`)
	c = &fakeClient{pages: [][]byte{unparseable, unparseable, unparseable, unparseable}}
	if posts, _ := Feed(context.Background(), c, 10); len(posts) != 0 || len(c.calls) != 2 {
		t.Errorf("got %d posts in %d calls", len(posts), len(c.calls))
	}
}

func TestFeedValidatesLimit(t *testing.T) {
	for _, limit := range []int{0, 51} {
		_, err := Feed(context.Background(), &fakeClient{}, limit)
		var e *errs.Error
		if !errors.As(err, &e) || e.Kind != errs.Usage {
			t.Errorf("limit %d: expected a usage error, got %v", limit, err)
		}
	}
}

func TestPublishSendsTextAndVisibility(t *testing.T) {
	c := &fakeClient{response: []byte(`{"data":{"status":{"urn":"urn:li:share:5","toastCtaText":"View post"},"*updateV2":"urn:li:fs_updateV2:(urn:li:activity:9,FEED_DETAIL)"}}`)}
	post, err := Publish(context.Background(), c, "Hello", ConnectionsOnly)
	if err != nil {
		t.Fatal(err)
	}
	payload := c.posted[0].(map[string]any)
	if payload["visibleToConnectionsOnly"] != true || payload["commentaryV2"].(map[string]any)["text"] != "Hello" {
		t.Errorf("unexpected payload %v", payload)
	}
	if post.URN != "urn:li:activity:9" || post.URL != "https://www.linkedin.com/feed/update/urn:li:activity:9/" {
		t.Errorf("unexpected post %+v", post)
	}
	if (ParsePublished([]byte(`{}`)) != PublishedPost{}) {
		t.Error("expected an empty result when no URN is present")
	}
}
