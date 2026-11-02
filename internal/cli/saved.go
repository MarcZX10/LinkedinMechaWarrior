package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

func (a *app) savedCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "saved",
		Short:   "Show the posts you saved",
		Long:    "Show the posts you saved on LinkedIn, most recently saved first. For saved jobs, use `lmw jobs tracked`.",
		Example: "  lmw saved -n 20\n  lmw saved --json",
		Args:    exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			posts, err := fetch(a, func(c *voyager.Client) ([]api.SavedPost, error) { return api.SavedPosts(cmd.Context(), c, limit) })
			if err != nil {
				return err
			}
			return printList(a, posts, "No saved posts.", output.SavedPosts)
		},
	}
	limitFlag(cmd, &limit, "posts", 10, api.MaxListItems)
	return cmd
}

func (a *app) jobsTrackedCommand() *cobra.Command {
	var limit int
	var stage string
	cmd := &cobra.Command{
		Use:     "tracked",
		Short:   "Show the jobs in your job tracker: saved, applied...",
		Example: "  lmw jobs tracked\n  lmw jobs tracked --stage applied",
		Args:    exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			jobs, err := fetch(a, func(c *voyager.Client) ([]api.Job, error) { return api.TrackedJobs(cmd.Context(), c, stage, limit) })
			if err != nil {
				return err
			}
			return printList(a, jobs, "No jobs at this stage.", output.Jobs)
		},
	}
	limitFlag(cmd, &limit, "jobs", 10, api.MaxListItems)
	cmd.Flags().StringVar(&stage, "stage", "saved", "stage: "+strings.Join(api.JobStageNames, ", "))
	return cmd
}

func (a *app) mcpSavedTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "get_saved_posts",
			Title:       "Saved posts",
			Description: "The posts the person saved on LinkedIn, most recently saved first.",
			ReadOnly:    true,
			Params:      []mcp.Param{limitParam("posts", 10, api.MaxListItems)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				items, err := fetch(a, func(c *voyager.Client) ([]api.SavedPost, error) { return api.SavedPosts(ctx, c, args.Int("limit")) })
				return listOf("posts", items, err)
			},
		},
		{
			Name:        "get_tracked_jobs",
			Title:       "Job tracker",
			Description: "The jobs in one stage of the person's job tracker: saved, in progress, applied, interviewing or archived.",
			ReadOnly:    true,
			Params: []mcp.Param{
				{Name: "stage", Type: "string", Description: "which stage", Enum: api.JobStageNames, Default: "saved"},
				limitParam("jobs", 10, api.MaxListItems),
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				items, err := fetch(a, func(c *voyager.Client) ([]api.Job, error) {
					return api.TrackedJobs(ctx, c, args.String("stage"), args.Int("limit"))
				})
				return listOf("jobs", items, err)
			},
		},
	}
}
