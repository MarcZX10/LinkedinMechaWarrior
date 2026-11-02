package cli

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

func (a *app) meCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show your profile",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			var profile api.Profile
			err := a.withClient(func(c *voyager.Client) (err error) {
				profile, err = api.Me(cmd.Context(), c)
				return err
			})
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, profile)
			}
			a.printf("%s\n", output.Profile(profile))
			return nil
		},
	}
}

func (a *app) feedCommand() *cobra.Command {
	var limit int
	var full bool
	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Show your home feed",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > api.MaxFeedPosts {
				return errs.New(errs.Usage, "--limit must be between 1 and %d", api.MaxFeedPosts)
			}
			var posts []api.FeedPost
			err := a.withClient(func(c *voyager.Client) (err error) {
				posts, err = api.Feed(cmd.Context(), c, limit)
				return err
			})
			if err != nil {
				return err
			}
			return a.printPosts(posts, full, "No posts found. If your feed is not empty, LinkedIn may have changed its API; see `lmw har --help`.")
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "number of posts (1-50)")
	cmd.Flags().BoolVar(&full, "full", false, "show complete post texts")
	return cmd
}

// printPosts prints posts as JSON or for people.
func (a *app) printPosts(posts []api.FeedPost, full bool, empty string) error {
	if a.jsonOut {
		if posts == nil {
			posts = []api.FeedPost{}
		}
		return output.JSON(a.stdout, posts)
	}
	if len(posts) == 0 {
		a.printf("%s\n", empty)
		return nil
	}
	formatted := make([]string, len(posts))
	for i, p := range posts {
		formatted[i] = output.FeedPost(p, full)
	}
	a.printf("%s\n", strings.Join(formatted, "\n\n"))
	return nil
}

func (a *app) mcpAccountTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "get_my_profile",
			Title:       "My profile",
			Description: "The person's own LinkedIn profile: name, headline, public id and address. Also a cheap way to check that the session works.",
			ReadOnly:    true,
			Run: func(ctx context.Context, _ mcp.Args) (any, error) {
				return fetch(a, func(c *voyager.Client) (api.Profile, error) { return api.Me(ctx, c) })
			},
		},
		{
			Name:        "get_feed",
			Title:       "Home feed",
			Description: "Recent posts from the person's home feed, newest first, with author, text, counts and address.",
			ReadOnly:    true,
			Params:      []mcp.Param{limitParam("posts", 10, api.MaxFeedPosts)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				posts, err := fetch(a, func(c *voyager.Client) ([]api.FeedPost, error) { return api.Feed(ctx, c, args.Int("limit")) })
				return listOf("posts", posts, err)
			},
		},
		{
			Name:        "get_request_budget",
			Title:       "Request budget",
			Description: "How many LinkedIn requests were sent in the last hour and day, the budgets, and any cooldown. Sends nothing to LinkedIn.",
			ReadOnly:    true,
			Run: func(context.Context, mcp.Args) (any, error) {
				return a.newPacer(a.cfg).Usage(), nil
			},
		},
		{
			Name:  "linkedin_api_get",
			Title: "Raw API GET",
			Description: "GET a path of LinkedIn's internal Voyager API (e.g. /me) and return the raw JSON. Only for data no other tool provides; " +
				"responses are large, so prefer the dedicated tools.",
			ReadOnly: true,
			Params: []mcp.Param{
				{Name: "path", Type: "string", Description: "API path such as /me or /identity/profiles/NAME/profileContactInfo", Required: true},
				{Name: "query", Type: "string", Description: "query string without the leading ?, e.g. count=10&q=chronFeed"},
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				query, err := parseQuery(args.String("query"))
				if err != nil {
					return nil, err
				}
				raw, err := fetch(a, func(c *voyager.Client) ([]byte, error) { return c.Get(ctx, args.String("path"), query) })
				if err != nil {
					return nil, err
				}
				return json.RawMessage(raw), nil
			},
		},
	}
}
