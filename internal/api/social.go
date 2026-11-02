package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/normalized"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// MaxComment is the longest comment LinkedIn accepts.
const MaxComment = 1250

// Comment is one comment on a post.
type Comment struct {
	URN            string `json:"urn,omitempty"`
	Author         string `json:"author,omitempty"`
	AuthorHeadline string `json:"author_headline,omitempty"`
	AuthorURL      string `json:"author_url,omitempty"`
	Text           string `json:"text"`
	Date           string `json:"date,omitempty"`
	Reactions      *int   `json:"reactions"`
	Replies        *int   `json:"replies"`
}

// Reaction is one member's reaction to a post.
type Reaction struct {
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	Headline string `json:"headline,omitempty"`
	URL      string `json:"url,omitempty"`
}

// ReactionTypes maps the names people use to LinkedIn's reaction types.
var ReactionTypes = map[string]string{
	"like":       "LIKE",
	"celebrate":  "PRAISE",
	"support":    "APPRECIATION",
	"love":       "EMPATHY",
	"insightful": "INTEREST",
	"funny":      "ENTERTAINMENT",
}

// ReactionNames lists the reaction names in the order LinkedIn shows them.
var ReactionNames = []string{"like", "celebrate", "support", "love", "insightful", "funny"}

// GetPost returns one post.
func GetPost(ctx context.Context, c Client, urn string) (FeedPost, error) {
	raw, err := updateQuery.Get(ctx, c, "(updateUrn:"+restliValue("urn:li:fsd_update:("+urn+",MESSAGING_RESHARE,EMPTY,DEFAULT,false)")+")")
	if err != nil {
		return FeedPost{}, err
	}
	posts, _ := ParseFeed(raw)
	for _, p := range posts {
		if p.URN == urn || p.URN == "" {
			return p, nil
		}
	}
	if len(posts) > 0 {
		return posts[0], nil
	}
	return FeedPost{}, errs.New(errs.API, "LinkedIn did not return the post %s; it may have been deleted or be hidden from you", urn)
}

// socialDetailURN names the comments thread of a post.
func socialDetailURN(post string) string {
	return "urn:li:fsd_socialDetail:(" + post + "," + post + ",urn:li:highlightedReply:-)"
}

// Comments returns the most relevant comments on a post.
func Comments(ctx context.Context, c Client, post string, limit int) ([]Comment, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	read := func(socialDetail string) ([]Comment, error) {
		return paginate(limit, func(start int) ([]Comment, int, error) {
			raw, err := commentsQuery.Get(ctx, c, fmt.Sprintf("(count:%d,numReplies:1,socialDetailUrn:%s,sortOrder:RELEVANCE,start:%d)",
				ListPageSize, restliValue(socialDetail), start))
			if err != nil {
				return nil, 0, err
			}
			found, next := ParseComments(raw, start)
			return found, next, nil
		})
	}
	found, err := read(socialDetailURN(post))
	if err != nil || len(found) > 0 {
		return found, err
	}
	// A repost keeps the comments of the original post.
	if t := postThread(ctx, c, post); t.socialDetail != socialDetailURN(post) {
		return read(t.socialDetail)
	}
	return found, nil
}

// ParseComments reads a page of comments that started at start, and returns
// where the next page starts.
func ParseComments(raw []byte, start int) ([]Comment, int) {
	doc := normalized.Parse(raw)
	items, n, coll := collection(doc)
	if coll == nil {
		items = doc.OfType(".Comment")
		n = len(items)
	}
	var out []Comment
	for _, item := range items {
		text := normalized.Text(item["commentary"])
		if text == "" {
			continue
		}
		commenter := doc.Ref(item, "commenter")
		counts := doc.Ref(doc.Ref(item, "socialDetail"), "totalSocialActivityCounts")
		out = append(out, Comment{
			URN:            normalized.String(item["entityUrn"]),
			Author:         normalized.Text(normalized.Dig(commenter, "title")),
			AuthorHeadline: normalized.Text(normalized.Dig(commenter, "subtitle")),
			AuthorURL:      cleanURL(normalized.String(normalized.Dig(commenter, "navigationUrl"))),
			Text:           text,
			Date:           millisDate(firstOf(item, "createdAt", "createdTime")),
			Reactions:      intPtr(counts["numLikes"]),
			Replies:        intPtr(counts["numComments"]),
		})
	}
	return out, pageEnd(coll, start, n, ListPageSize)
}

// Reactions returns who reacted to a post, and how.
func Reactions(ctx context.Context, c Client, post string, limit int) ([]Reaction, error) {
	if err := checkLimit(limit, MaxListItems); err != nil {
		return nil, err
	}
	read := func(thread string) ([]Reaction, error) {
		return paginate(limit, func(start int) ([]Reaction, int, error) {
			raw, err := reactionsQuery.Get(ctx, c, fmt.Sprintf("(count:%d,start:%d,threadUrn:%s)", ListPageSize, start, restliValue(thread)))
			if err != nil {
				return nil, 0, err
			}
			found, next := ParseReactions(raw, start)
			return found, next, nil
		})
	}
	found, err := read(post)
	if err != nil || len(found) > 0 {
		return found, err
	}
	// A repost keeps the reactions of the original post.
	if t := postThread(ctx, c, post); t.urn != post {
		return read(t.urn)
	}
	return found, nil
}

// ParseReactions reads a page of reactions that started at start, and
// returns where the next page starts.
func ParseReactions(raw []byte, start int) ([]Reaction, int) {
	doc := normalized.Parse(raw)
	items, n, coll := collection(doc)
	if coll == nil {
		items = doc.OfType(".Reaction")
		n = len(items)
	}
	names := map[string]string{}
	for name, kind := range ReactionTypes {
		names[kind] = name
	}
	var out []Reaction
	for _, item := range items {
		kind := normalized.String(item["reactionType"])
		lockup := doc.Ref(item, "reactorLockup")
		r := Reaction{
			Type:     firstNonEmpty(names[kind], strings.ToLower(kind)),
			Name:     normalized.Text(normalized.Dig(lockup, "title")),
			Headline: normalized.Text(normalized.Dig(lockup, "subtitle")),
			URL:      cleanURL(normalized.String(normalized.Dig(lockup, "navigationUrl"))),
		}
		if r.Name == "" {
			if person := personIn(doc, item, "reactor", "actor", "actorUrn"); person != nil {
				r.Name, r.Headline, r.URL, _ = personSummary(person)
			}
		}
		if r.Type != "" {
			out = append(out, r)
		}
	}
	return out, pageEnd(coll, start, n, ListPageSize)
}

// React adds your reaction (a name from ReactionNames) to a post or comment.
func React(ctx context.Context, c Client, post, reaction string) error {
	kind, ok := ReactionTypes[strings.ToLower(reaction)]
	if !ok {
		return errs.New(errs.Usage, "unknown reaction %q; use one of: %s", reaction, strings.Join(ReactionNames, ", "))
	}
	_, err := reactQuery.Execute(ctx, c, map[string]any{"entity": map[string]any{"reactionType": kind}, "threadUrn": postThread(ctx, c, post).urn})
	return err
}

// Unreact removes your reaction from a post or comment.
func Unreact(ctx context.Context, c Client, post string) error {
	_, err := unreactQuery.Execute(ctx, c, map[string]any{"threadUrn": postThread(ctx, c, post).urn})
	return err
}

// CommentOn comments on a post. LinkedIn sometimes answers with a server
// error after the comment was created, so a failure is never retried.
func CommentOn(ctx context.Context, c Client, post, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errs.New(errs.Validation, "the comment is empty")
	}
	if n := len([]rune(text)); n > MaxComment {
		return "", errs.New(errs.Validation, "the comment is %d characters long; the limit is %d", n, MaxComment)
	}
	raw, err := c.Post(ctx, "/voyagerSocialDashNormComments", voyager.Params{
		{Key: "decorationId", Value: "com.linkedin.voyager.dash.deco.social.NormComment-43"},
	}, map[string]any{
		"commentary": map[string]any{"text": text, "attributesV2": []any{}, "$type": "com.linkedin.voyager.dash.common.text.TextViewModel"},
		"threadUrn":  postThread(ctx, c, post).urn,
	})
	if err != nil {
		var httpErr *errs.HTTPError
		if errors.As(err, &httpErr) && httpErr.Status >= 500 {
			return "", errs.Wrap(errs.API, err, "%v. The comment may have been posted anyway: check the post before trying again", err)
		}
		return "", err
	}
	if m := commentURN.FindString(string(raw)); m != "" {
		return m, nil
	}
	return "", nil
}

var commentURN = regexp.MustCompile(`urn:li:fsd_comment:\([^)]*\)`)

// DeletePost deletes one of your posts.
func DeletePost(ctx context.Context, c Client, post string) error {
	_, err := c.Delete(ctx, "/contentcreation/normShares/"+url.PathEscape(post), nil)
	return err
}

// cleanURL drops the tracking parameters of a LinkedIn address.
func cleanURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Host, "linkedin.com") {
		return raw
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// thread is how LinkedIn names the comments and reactions of a post. A
// repost shares the thread of the original post, so the address of a post
// in the feed is not always its thread; the post's SocialDetail says which.
type thread struct {
	socialDetail string // what the comments query takes
	urn          string // what reactions and new comments take
}

// postThread finds the thread of a post or comment. For posts it asks
// LinkedIn (one request); if that fails, it names the thread after the post.
// Reads call it only when the post's own thread came back empty.
func postThread(ctx context.Context, c Client, post string) thread {
	fallback := thread{socialDetail: socialDetailURN(post), urn: post}
	if IsComment(post) {
		return fallback
	}
	raw, err := updateQuery.Get(ctx, c, "(updateUrn:"+restliValue("urn:li:fsd_update:("+post+",MESSAGING_RESHARE,EMPTY,DEFAULT,false)")+")")
	if err != nil {
		return fallback
	}
	if t := threadIn(raw, post); t.urn != "" {
		return t
	}
	return fallback
}

// threadIn reads the thread of post from an answer that holds the post.
func threadIn(raw []byte, post string) thread {
	doc := normalized.Parse(raw)
	for _, update := range append(doc.OfType("feed.Update"), doc.OfType("UpdateV2")...) {
		if !strings.Contains(normalized.String(update["entityUrn"]), post+",") {
			continue
		}
		if detail := doc.Ref(update, "socialDetail"); detail != nil {
			t := thread{socialDetail: normalized.String(detail["entityUrn"]), urn: normalized.String(detail["threadUrn"])}
			if strings.HasPrefix(t.socialDetail, "urn:li:fsd_socialDetail:") && t.urn != "" {
				return t
			}
		}
	}
	return thread{}
}
