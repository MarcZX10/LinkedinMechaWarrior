package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
)

// Item is one thing a check saw. Key tells it apart from the others.
type Item struct {
	Key, Title, Text, URL string
}

// Kind is one kind of watch.
type Kind struct {
	Name string
	// About says what it watches; Target what its target is, if it has one.
	About, Target string
	// Cost is how many requests a check makes, at most.
	Cost    int
	Prepare func(ctx context.Context, c api.Client, w *Watch) error
	Check   func(ctx context.Context, c api.Client, w *Watch) ([]Item, error)
	// Fresh picks the items to tell about; nil means the unseen ones.
	Fresh func(seen []string, items []Item) []Item
	// Replace makes a check remember only what it saw last, for kinds that
	// watch a state (a profile, counts) rather than a stream.
	Replace bool
}

func (k Kind) fresh(seen []string, items []Item) []Item {
	if k.Fresh != nil {
		return k.Fresh(seen, items)
	}
	known := map[string]bool{}
	for _, key := range seen {
		known[key] = true
	}
	var fresh []Item
	for _, item := range items {
		if !known[item.Key] {
			fresh = append(fresh, item)
		}
	}
	return fresh
}

// Kinds lists the kinds of watches.
var Kinds = []Kind{
	{
		Name: "posts", About: "new posts by a member", Target: "a profile (linkedin.com/in/NAME)", Cost: 1,
		Prepare: prepareProfile,
		Check: func(ctx context.Context, c api.Client, w *Watch) ([]Item, error) {
			posts, err := api.ProfilePosts(ctx, c, profileOf(w), 5)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, p := range posts {
				if p.URN == "" {
					continue
				}
				title := "New post by " + firstNonEmpty(p.Author, w.Target)
				if p.ResharedAuthor != "" {
					title = firstNonEmpty(p.Author, w.Target) + " reposted " + p.ResharedAuthor
				}
				items = append(items, Item{Key: p.URN, Title: title, Text: clip(p.Text, 280), URL: p.URL})
			}
			return items, nil
		},
	},
	{
		Name: "profile", About: "changes to a member's headline or current job", Target: "a profile (linkedin.com/in/NAME)", Cost: 1,
		Prepare: prepareProfile, Replace: true,
		Check: func(ctx context.Context, c api.Client, w *Watch) ([]Item, error) {
			p, err := api.GetProfile(ctx, c, profileOf(w))
			if err != nil {
				return nil, err
			}
			now := ""
			if len(p.Experience) > 0 {
				now = strings.TrimSuffix(p.Experience[0].Title+" at "+p.Experience[0].Company, " at ")
			}
			text := "Headline: " + p.Headline
			if now != "" {
				text += "\nNow: " + now
			}
			return []Item{{Key: p.Headline + "|" + now, Title: firstNonEmpty(p.Name, w.Target) + " updated their profile", Text: text, URL: p.URL}}, nil
		},
	},
	{
		Name: "comments", About: "new comments on a post", Target: "a post (its link or URN)", Cost: 2,
		Prepare: func(_ context.Context, _ api.Client, w *Watch) error {
			urn, err := api.ParsePost(w.Target)
			w.Target = urn
			return err
		},
		Check: func(ctx context.Context, c api.Client, w *Watch) ([]Item, error) {
			comments, err := api.Comments(ctx, c, w.Target, 10)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, comment := range comments {
				items = append(items, Item{Key: firstNonEmpty(comment.URN, hash(comment.Author+comment.Text)), Title: "New comment by " + comment.Author,
					Text: clip(comment.Text, 280), URL: "https://www.linkedin.com/feed/update/" + w.Target + "/"})
			}
			return items, nil
		},
	},
	{
		Name: "jobs", About: "new job postings for a search", Target: "the search keywords", Cost: 1,
		Check: func(ctx context.Context, c api.Client, w *Watch) ([]Item, error) {
			search := api.JobSearch{Keywords: w.Target, Location: w.Params["location"], Posted: w.Params["posted"]}
			if workplace := w.Params["workplace"]; workplace != "" {
				search.Workplace = strings.Split(workplace, ",")
			}
			jobs, err := api.SearchJobs(ctx, c, search, 10)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, j := range jobs {
				title := "New job: " + j.Title
				if j.Company != "" {
					title += " at " + j.Company
				}
				items = append(items, Item{Key: j.ID, Title: title, Text: joinNonEmpty(" · ", j.Location, j.Workplace, j.Listed), URL: j.URL})
			}
			return items, nil
		},
	},
	{
		Name: "views", About: "new people viewing your profile", Cost: 1,
		Check: func(ctx context.Context, c api.Client, _ *Watch) ([]Item, error) {
			views, err := api.GetProfileViews(ctx, c)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, v := range views.Viewers {
				items = append(items, Item{Key: firstNonEmpty(v.URL, v.Name) + "@" + v.Viewed, Title: v.Name + " viewed your profile",
					Text: v.Headline, URL: v.URL})
			}
			return items, nil
		},
	},
	{
		Name: "invitations", About: "new connection requests", Cost: 1,
		Check: func(ctx context.Context, c api.Client, _ *Watch) ([]Item, error) {
			invitations, err := api.Invitations(ctx, c, 10)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, inv := range invitations {
				items = append(items, Item{Key: inv.ID, Title: "Connection request from " + inv.Name,
					Text: joinNonEmpty("\n", inv.Headline, clip(inv.Message, 280)), URL: inv.Profile})
			}
			return items, nil
		},
	},
	{
		Name: "messages", About: "new messages in your inbox", Cost: 2,
		Check: func(ctx context.Context, c api.Client, _ *Watch) ([]Item, error) {
			conversations, err := api.Conversations(ctx, c, "inbox", 10)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, conv := range conversations {
				if conv.Unread == 0 {
					continue
				}
				from := firstNonEmpty(conv.LastSender, strings.Join(conv.With, ", "), conv.Title)
				items = append(items, Item{Key: conv.ID + "@" + conv.Date + ":" + hash(conv.LastMessage), Title: "Message from " + from,
					Text: clip(conv.LastMessage, 280), URL: conv.URL})
			}
			return items, nil
		},
	},
	{
		Name: "notifications", About: "new notifications", Cost: 1,
		Check: func(ctx context.Context, c api.Client, _ *Watch) ([]Item, error) {
			notifications, err := api.Notifications(ctx, c, 10)
			if err != nil {
				return nil, err
			}
			var items []Item
			for _, n := range notifications {
				items = append(items, Item{Key: hash(n.Text + n.URL), Title: "LinkedIn notification", Text: clip(n.Text, 280), URL: n.URL})
			}
			return items, nil
		},
	},
	{
		Name: "unread", About: "more unread notifications, messages or invitations (the top bar badges)", Cost: 1, Replace: true,
		Check: func(ctx context.Context, c api.Client, _ *Watch) ([]Item, error) {
			u, err := api.UnreadCounts(ctx, c, false)
			if err != nil {
				return nil, err
			}
			return []Item{
				{Key: "notifications=" + strconv.Itoa(u.Notifications), Title: plural(u.Notifications, "unread notification"), URL: "https://www.linkedin.com/notifications/"},
				{Key: "messages=" + strconv.Itoa(u.Messages), Title: plural(u.Messages, "unread message"), URL: "https://www.linkedin.com/messaging/"},
				{Key: "network=" + strconv.Itoa(u.Network), Title: plural(u.Network, "network update"), URL: "https://www.linkedin.com/mynetwork/"},
			}, nil
		},
		// A count tells only when it went up.
		Fresh: func(seen []string, items []Item) []Item {
			before := map[string]int{}
			for _, key := range seen {
				name, n, _ := strings.Cut(key, "=")
				before[name], _ = strconv.Atoi(n)
			}
			var fresh []Item
			for _, item := range items {
				name, n, _ := strings.Cut(item.Key, "=")
				if count, _ := strconv.Atoi(n); count > before[name] {
					fresh = append(fresh, item)
				}
			}
			return fresh
		},
	},
}

// Find returns the kind called name.
func Find(name string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

// KindNames lists the kinds, for messages.
func KindNames() string {
	names := make([]string, len(Kinds))
	for i, k := range Kinds {
		names[i] = k.Name
	}
	return strings.Join(names, ", ")
}

// prepareProfile finds the member id of the profile once, so checks need
// no lookup.
func prepareProfile(ctx context.Context, c api.Client, w *Watch) error {
	ref, err := api.ParseProfile(w.Target)
	if err != nil {
		return err
	}
	urn, err := api.MemberURN(ctx, c, ref)
	if err != nil {
		return err
	}
	w.Target = ref.String()
	if w.Params == nil {
		w.Params = map[string]string{}
	}
	w.Params["member_id"] = strings.TrimPrefix(urn, "urn:li:fsd_profile:")
	return nil
}

func profileOf(w *Watch) api.ProfileRef {
	ref, _ := api.ParseProfile(w.Target)
	if id := w.Params["member_id"]; id != "" {
		ref.MemberID = id
	}
	return ref
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n-1])) + "…"
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func joinNonEmpty(sep string, values ...string) string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}
