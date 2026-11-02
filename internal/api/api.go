// Package api implements LinkedIn operations on top of the Voyager client.
//
// Endpoints and response shapes are undocumented. Parsers never assume a field
// exists; when LinkedIn changes something, use `lmw api get` and
// `lmw har inspect` to look at the current responses.
package api

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

// Client is the part of voyager.Client the operations use.
type Client interface {
	Get(ctx context.Context, path string, params voyager.Params) ([]byte, error)
	Post(ctx context.Context, path string, params voyager.Params, payload any) ([]byte, error)
	Delete(ctx context.Context, path string, params voyager.Params) ([]byte, error)
}

const (
	// FeedPageSize matches the page size LinkedIn's web app uses.
	FeedPageSize = 10
	// MaxFeedPosts caps how many posts one command may fetch.
	MaxFeedPosts = 50
)

// Profile is a LinkedIn member profile.
type Profile struct {
	URN       string `json:"urn,omitempty"`
	PublicID  string `json:"public_id,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Name      string `json:"name"`
	Headline  string `json:"headline,omitempty"`
	URL       string `json:"url,omitempty"`
}

// FeedPost is one post in the home feed.
type FeedPost struct {
	URN            string `json:"urn,omitempty"`
	URL            string `json:"url,omitempty"`
	Author         string `json:"author,omitempty"`
	AuthorHeadline string `json:"author_headline,omitempty"`
	Age            string `json:"age,omitempty"`
	Text           string `json:"text,omitempty"`
	Context        string `json:"context,omitempty"`
	ResharedAuthor string `json:"reshared_author,omitempty"`
	Reactions      *int   `json:"reactions"`
	Comments       *int   `json:"comments"`
	Reposts        *int   `json:"reposts"`
	Promoted       bool   `json:"promoted"`
}

// Me returns your own profile.
func Me(ctx context.Context, c Client) (Profile, error) {
	raw, err := c.Get(ctx, "/me", nil)
	if err != nil {
		return Profile{}, err
	}
	return ParseMe(raw)
}

// ParseMe reads the /me response.
func ParseMe(raw []byte) (Profile, error) {
	doc := normalized.Parse(raw)
	mini := doc.Ref(doc.Data, "miniProfile")
	if mini == nil {
		if candidates := doc.OfType("MiniProfile"); len(candidates) > 0 {
			mini = candidates[0]
		}
	}
	if mini == nil {
		return Profile{}, errs.New(errs.API, "unexpected response from /me: no profile found. Inspect it with `lmw api get /me`")
	}
	p := Profile{
		URN:       firstNonEmpty(normalized.String(mini["dashEntityUrn"]), normalized.String(mini["entityUrn"])),
		PublicID:  normalized.String(mini["publicIdentifier"]),
		FirstName: normalized.String(mini["firstName"]),
		LastName:  normalized.String(mini["lastName"]),
		Headline:  normalized.String(mini["occupation"]),
	}
	p.Name = strings.TrimSpace(p.FirstName + " " + p.LastName)
	if p.Name == "" {
		p.Name = "(unknown)"
	}
	if p.PublicID != "" {
		p.URL = "https://www.linkedin.com/in/" + p.PublicID + "/"
	}
	return p, nil
}

// Feed returns up to limit posts from the home feed.
func Feed(ctx context.Context, c Client, limit int) ([]FeedPost, error) {
	if limit < 1 || limit > MaxFeedPosts {
		return nil, errs.New(errs.Usage, "--limit must be between 1 and %d", MaxFeedPosts)
	}
	return paginatePosts(limit, func(start int) ([]byte, error) {
		return c.Get(ctx, "/feed/updatesV2", voyager.Params{
			{Key: "count", Value: strconv.Itoa(FeedPageSize)},
			{Key: "q", Value: "chronFeed"},
			{Key: "start", Value: strconv.Itoa(start)},
		})
	})
}

// paginatePosts fetches pages of updates until it has limit posts.
func paginatePosts(limit int, page func(start int) ([]byte, error)) ([]FeedPost, error) {
	var posts []FeedPost
	start := 0
	// Hard cap on pages, in case pages keep coming back with elements we can't parse.
	maxPages := (limit+FeedPageSize-1)/FeedPageSize + 1
	for range maxPages {
		raw, err := page(start)
		if err != nil {
			return nil, err
		}
		found, elements := ParseFeed(raw)
		posts = append(posts, found...)
		if elements == 0 || len(posts) >= limit {
			break
		}
		start += elements
	}
	if len(posts) > limit {
		posts = posts[:limit]
	}
	return posts, nil
}

// ParseFeed returns the posts on one feed page and how many elements the page had.
func ParseFeed(raw []byte) ([]FeedPost, int) {
	doc := normalized.Parse(raw)
	var updates []normalized.Object
	elements := 0
	if urns, ok := doc.Data["*elements"].([]any); ok {
		elements = len(urns)
		for _, urn := range urns {
			if update := doc.Get(urn); update != nil {
				updates = append(updates, update)
			}
		}
	} else {
		updates = doc.OfType("UpdateV2")
		elements = len(updates)
	}

	var posts []FeedPost
	for _, update := range updates {
		if post, ok := parseUpdate(doc, update); ok {
			posts = append(posts, post)
		}
	}
	return posts, elements
}

var activityURN = regexp.MustCompile(`urn:li:activity:\d+`)

func parseUpdate(doc *normalized.Doc, update normalized.Object) (FeedPost, bool) {
	actor, _ := update["actor"].(normalized.Object)
	post := FeedPost{
		Author:         normalized.Text(normalized.Dig(actor, "name")),
		AuthorHeadline: normalized.Text(normalized.Dig(actor, "description")),
		Text:           normalized.Text(update["commentary"]),
		Context:        normalized.Text(normalized.Dig(update, "header", "text")),
	}
	if reshared := doc.Ref(update, "resharedUpdate"); reshared != nil {
		post.ResharedAuthor = normalized.Text(normalized.Dig(reshared, "actor", "name"))
		if post.Text == "" {
			post.Text = normalized.Text(reshared["commentary"])
		}
	}
	if post.Author == "" && post.Text == "" {
		return FeedPost{}, false
	}

	age := normalized.Text(normalized.Dig(actor, "subDescription"))
	post.Promoted = strings.Contains(strings.ToLower(age+" "+post.AuthorHeadline), "promoted")
	post.Age = strings.TrimSpace(strings.Split(age, "•")[0])
	if strings.EqualFold(post.Age, "promoted") {
		// Sponsored posts show "Promoted" where the age would be.
		post.Age = ""
	}

	counts := doc.Ref(doc.Ref(update, "socialDetail"), "totalSocialActivityCounts")
	post.Reactions = intPtr(counts["numLikes"])
	post.Comments = intPtr(counts["numComments"])
	post.Reposts = intPtr(counts["numShares"])

	for _, candidate := range []any{normalized.Dig(update, "updateMetadata", "urn"), update["entityUrn"], update["urn"]} {
		if match := activityURN.FindString(normalized.String(candidate)); match != "" {
			post.URN = match
			post.URL = "https://www.linkedin.com/feed/update/" + match + "/"
			break
		}
	}
	return post, true
}

func intPtr(value any) *int {
	if n, ok := normalized.Int(value); ok {
		return &n
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
