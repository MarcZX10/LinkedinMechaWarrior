package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

func (a *app) profileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Look at a member's profile, contact info and posts",
		Long: "Look at a member's profile, contact info and posts.\n\n" +
			"PROFILE is the address of the profile (linkedin.com/in/NAME), NAME itself,\n" +
			"or a member URN.",
	}
	cmd.AddCommand(a.profileShowCommand(), a.profileContactCommand(), a.profilePostsCommand())
	return cmd
}

func (a *app) profileShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show PROFILE",
		Short: "Show a profile: headline, about, experience, education, skills",
		Example: "  lmw profile show ada-lovelace\n" +
			"  lmw profile show https://www.linkedin.com/in/ada-lovelace/ --json",
		Args: exactArgs(1, "one profile, such as ada-lovelace"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := api.ParseProfile(args[0])
			if err != nil {
				return err
			}
			profile, err := fetch(a, func(c *voyager.Client) (api.ProfileDetails, error) { return api.GetProfile(cmd.Context(), c, ref) })
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, profile)
			}
			a.printf("%s\n", output.ProfileDetails(profile))
			return nil
		},
	}
}

func (a *app) profileContactCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "contact PROFILE",
		Short: "Show the contact info a member shares with you",
		Args:  exactArgs(1, "one profile, such as ada-lovelace"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := api.ParseProfile(args[0])
			if err != nil {
				return err
			}
			info, err := fetch(a, func(c *voyager.Client) (api.ContactInfo, error) { return api.GetContactInfo(cmd.Context(), c, ref) })
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, info)
			}
			a.printf("%s\n", output.ContactInfo(info))
			return nil
		},
	}
}

// profileParam is the argument naming a member in MCP tools.
var profileParam = mcp.Param{Name: "profile", Type: "string", Required: true,
	Description: "the member's profile address (linkedin.com/in/NAME), NAME, or member URN"}

func (a *app) mcpProfileTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "get_profile",
			Title:       "Member profile",
			Description: "A member's profile: name, headline, location, about, experience, education and skills.",
			ReadOnly:    true,
			Params:      []mcp.Param{profileParam},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				ref, err := api.ParseProfile(args.String("profile"))
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.ProfileDetails, error) { return api.GetProfile(ctx, c, ref) })
			},
		},
		{
			Name:        "get_profile_posts",
			Title:       "Member posts",
			Description: "A member's recent posts and reposts, newest first.",
			ReadOnly:    true,
			Params:      []mcp.Param{profileParam, limitParam("posts", 10, api.MaxFeedPosts)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				ref, err := api.ParseProfile(args.String("profile"))
				if err != nil {
					return nil, err
				}
				posts, err := fetch(a, func(c *voyager.Client) ([]api.FeedPost, error) {
					return api.ProfilePosts(ctx, c, ref, args.Int("limit"))
				})
				return listOf("posts", posts, err)
			},
		},
		{
			Name:        "get_contact_info",
			Title:       "Member contact info",
			Description: "The contact info (email, phones, websites...) a member shares with the person. Usually only shared with 1st-degree connections.",
			ReadOnly:    true,
			Params:      []mcp.Param{profileParam},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				ref, err := api.ParseProfile(args.String("profile"))
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.ContactInfo, error) { return api.GetContactInfo(ctx, c, ref) })
			},
		},
	}
}

func (a *app) profilePostsCommand() *cobra.Command {
	var limit int
	var full bool
	cmd := &cobra.Command{
		Use:     "posts PROFILE",
		Short:   "Show a member's recent posts",
		Example: "  lmw profile posts ada-lovelace -n 5",
		Args:    exactArgs(1, "one profile, such as ada-lovelace"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := api.ParseProfile(args[0])
			if err != nil {
				return err
			}
			posts, err := fetch(a, func(c *voyager.Client) ([]api.FeedPost, error) {
				return api.ProfilePosts(cmd.Context(), c, ref, limit)
			})
			if err != nil {
				return err
			}
			return a.printPosts(posts, full, "No posts found.")
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "number of posts (1-50)")
	cmd.Flags().BoolVar(&full, "full", false, "show complete post texts")
	return cmd
}
