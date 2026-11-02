package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

const postArgHelp = "one post: its address or URN (urn:li:activity:...)"

func (a *app) postShowCommand() *cobra.Command {
	var comments int
	var full bool
	cmd := &cobra.Command{
		Use:   "show POST",
		Short: "Show a post and its most relevant comments",
		Long: "Show a post and its most relevant comments.\n\n" +
			"POST is the post's address (copied from the browser) or URN.",
		Example: "  lmw post show https://www.linkedin.com/feed/update/urn:li:activity:7123456789012345678/\n" +
			"  lmw post show urn:li:activity:7123456789012345678 --comments 0",
		Args: exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			if comments < 0 || comments > api.MaxListItems {
				return errUsage("--comments must be between 0 and %d", api.MaxListItems)
			}
			var post api.FeedPost
			var thread []api.Comment
			err = a.withClient(func(c *voyager.Client) error {
				var err error
				if post, err = api.GetPost(cmd.Context(), c, urn); err != nil || comments == 0 {
					return err
				}
				thread, err = api.Comments(cmd.Context(), c, urn, comments)
				return err
			})
			if err != nil {
				return err
			}
			if thread == nil {
				thread = []api.Comment{}
			}
			if a.jsonOut {
				return output.JSON(a.stdout, map[string]any{"post": post, "comments": thread})
			}
			a.printf("%s\n", output.FeedPost(post, full))
			if len(thread) > 0 {
				a.printf("\nComments\n\n%s\n", output.Comments(thread))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&comments, "comments", 5, fmt.Sprintf("number of comments to show (0-%d)", api.MaxListItems))
	cmd.Flags().BoolVar(&full, "full", false, "show the complete post text")
	return cmd
}

func (a *app) postCommentsCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "comments POST",
		Short: "Show the most relevant comments on a post",
		Args:  exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			comments, err := fetch(a, func(c *voyager.Client) ([]api.Comment, error) { return api.Comments(cmd.Context(), c, urn, limit) })
			if err != nil {
				return err
			}
			return printList(a, comments, "No comments.", output.Comments)
		},
	}
	limitFlag(cmd, &limit, "comments", 10, api.MaxListItems)
	return cmd
}

func (a *app) postReactionsCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "reactions POST",
		Short: "Show who reacted to a post, and how",
		Args:  exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			reactions, err := fetch(a, func(c *voyager.Client) ([]api.Reaction, error) { return api.Reactions(cmd.Context(), c, urn, limit) })
			if err != nil {
				return err
			}
			return printList(a, reactions, "No reactions.", output.Reactions)
		},
	}
	limitFlag(cmd, &limit, "reactions", 10, api.MaxListItems)
	return cmd
}

func (a *app) postReactCommand() *cobra.Command {
	var reaction string
	cmd := &cobra.Command{
		Use:     "react POST",
		Short:   "React to a post (asks for confirmation)",
		Example: "  lmw post react urn:li:activity:7123456789012345678 --type celebrate",
		Args:    exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			reaction = strings.ToLower(reaction)
			if _, ok := api.ReactionTypes[reaction]; !ok {
				return errUsage("unknown reaction %q; use one of: %s", reaction, strings.Join(api.ReactionNames, ", "))
			}
			if ok, err := a.approve(fmt.Sprintf("React %q to %s.", reaction, urn), "react"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.React(cmd.Context(), c, urn, reaction) }); err != nil {
				return err
			}
			return a.done("Reacted "+reaction+".", map[string]any{"post": urn, "reaction": reaction})
		},
	}
	cmd.Flags().StringVar(&reaction, "type", "like", "reaction: "+strings.Join(api.ReactionNames, ", "))
	return cmd
}

func (a *app) postUnreactCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unreact POST",
		Short: "Remove your reaction from a post (asks for confirmation)",
		Args:  exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			if ok, err := a.approve("Remove your reaction from "+urn+".", "unreact"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.Unreact(cmd.Context(), c, urn) }); err != nil {
				return err
			}
			return a.done("Reaction removed.", map[string]any{"post": urn, "reaction": nil})
		},
	}
}

func (a *app) postCommentCommand() *cobra.Command {
	source := textSource{what: "comment"}
	cmd := &cobra.Command{
		Use:   "comment POST",
		Short: "Comment on a post (asks for confirmation)",
		Long: "Comment on a post, after showing the comment and asking you to type 'comment'.\n\n" +
			"LinkedIn sometimes reports an error after the comment was posted, so lmw never\n" +
			"retries: check the post before trying again.",
		Example: "  lmw post comment urn:li:activity:7123456789012345678 --text \"Great write-up, thanks!\"",
		Args:    exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			text, err := source.read()
			if err != nil {
				return err
			}
			if ok, err := a.approve(fmt.Sprintf("Comment on %s:\n%s", urn, text), "comment"); err != nil || !ok {
				return err
			}
			comment, err := fetch(a, func(c *voyager.Client) (string, error) { return api.CommentOn(cmd.Context(), c, urn, text) })
			if err != nil {
				return err
			}
			return a.done("Comment posted.", map[string]any{"post": urn, "comment": comment})
		},
	}
	source.register(cmd)
	return cmd
}

func (a *app) postDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete POST",
		Short: "Delete one of your posts (asks for confirmation)",
		Args:  exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			if ok, err := a.approve("Delete your post "+urn+". This cannot be undone.", "delete"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.DeletePost(cmd.Context(), c, urn) }); err != nil {
				return err
			}
			return a.done("Post deleted.", map[string]any{"post": urn, "deleted": true})
		},
	}
}

// postParam is the argument naming a post in MCP tools.
var postParam = mcp.Param{Name: "post", Type: "string", Required: true,
	Description: "the post's address (linkedin.com/feed/update/urn:li:activity:...) or URN"}

func (a *app) mcpSocialTools() []mcp.Tool {
	parsePost := func(args mcp.Args) (string, error) { return api.ParsePost(args.String("post")) }
	return []mcp.Tool{
		{
			Name:        "get_post",
			Title:       "Post",
			Description: "One post with its author, text and counts.",
			ReadOnly:    true,
			Params:      []mcp.Param{postParam},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.FeedPost, error) { return api.GetPost(ctx, c, urn) })
			},
		},
		{
			Name:        "get_post_comments",
			Title:       "Post comments",
			Description: "The most relevant comments on a post, with their authors.",
			ReadOnly:    true,
			Params:      []mcp.Param{postParam, limitParam("comments", 10, api.MaxListItems)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				items, err := fetch(a, func(c *voyager.Client) ([]api.Comment, error) { return api.Comments(ctx, c, urn, args.Int("limit")) })
				return listOf("comments", items, err)
			},
		},
		{
			Name:        "get_post_reactions",
			Title:       "Post reactions",
			Description: "Who reacted to a post, and with which reaction.",
			ReadOnly:    true,
			Params:      []mcp.Param{postParam, limitParam("reactions", 10, api.MaxListItems)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				items, err := fetch(a, func(c *voyager.Client) ([]api.Reaction, error) { return api.Reactions(ctx, c, urn, args.Int("limit")) })
				return listOf("reactions", items, err)
			},
		},
		{
			Name:        "react_to_post",
			Title:       "React to a post",
			Description: "Add the person's reaction to a post.",
			Params: []mcp.Param{postParam,
				{Name: "reaction", Type: "string", Description: "which reaction", Enum: api.ReactionNames, Default: "like"}},
			Confirm: func(args mcp.Args) (string, error) {
				return fmt.Sprintf("React %q to the LinkedIn post %s?", args.String("reaction"), args.String("post")), nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.React(ctx, c, urn, args.String("reaction")) }); err != nil {
					return nil, err
				}
				return map[string]any{"post": urn, "reaction": args.String("reaction")}, nil
			},
		},
		{
			Name:        "remove_reaction",
			Title:       "Remove a reaction",
			Description: "Remove the person's reaction from a post.",
			Params:      []mcp.Param{postParam},
			Confirm: func(args mcp.Args) (string, error) {
				return "Remove your reaction from the LinkedIn post " + args.String("post") + "?", nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.Unreact(ctx, c, urn) }); err != nil {
					return nil, err
				}
				return map[string]any{"post": urn, "reaction": nil}, nil
			},
		},
		{
			Name:  "comment_on_post",
			Title: "Comment on a post",
			Description: "Post a comment as the person. Show them the exact text first. If LinkedIn reports an error, " +
				"the comment may have been posted anyway: check with get_post_comments before trying again.",
			Params: []mcp.Param{postParam,
				{Name: "text", Type: "string", Required: true, Description: fmt.Sprintf("the comment, at most %d characters", api.MaxComment)}},
			Confirm: func(args mcp.Args) (string, error) {
				return "Post this comment on " + args.String("post") + "?\n\n" + args.String("text"), nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				comment, err := fetch(a, func(c *voyager.Client) (string, error) { return api.CommentOn(ctx, c, urn, args.String("text")) })
				if err != nil {
					return nil, err
				}
				return map[string]any{"post": urn, "comment": comment}, nil
			},
		},
		{
			Name:        "delete_post",
			Title:       "Delete a post",
			Description: "Delete one of the person's own posts. This cannot be undone.",
			Destructive: true,
			Params:      []mcp.Param{postParam},
			Confirm: func(args mcp.Args) (string, error) {
				return "Delete your LinkedIn post " + args.String("post") + "? This cannot be undone.", nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := parsePost(args)
				if err != nil {
					return nil, err
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.DeletePost(ctx, c, urn) }); err != nil {
					return nil, err
				}
				return map[string]any{"post": urn, "deleted": true}, nil
			},
		},
	}
}
