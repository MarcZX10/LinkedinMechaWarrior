package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

func searchTypes() []string {
	names := make([]string, 0, len(api.SearchTypes))
	for name := range api.SearchTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a *app) searchCommand() *cobra.Command {
	var limit int
	var network []string
	cmd := &cobra.Command{
		Use:   "search TYPE KEYWORDS...",
		Short: "Search people, companies, schools or groups",
		Long: "Search like the search box of the website. TYPE is " + strings.Join(searchTypes(), ", ") + ".\n\n" +
			"Posts can't be searched: the website serves that search in a format lmw\n" +
			"does not read. For jobs, use `lmw jobs search`.",
		Example: "  lmw search people grace hopper\n" +
			"  lmw search people \"go developer\" --network 2nd -n 20\n" +
			"  lmw search companies analytical engines",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errUsage("expected what to search (%s) and some keywords", strings.Join(searchTypes(), ", "))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := fetch(a, func(c *voyager.Client) ([]api.SearchResult, error) {
				return api.Search(cmd.Context(), c, args[0], strings.Join(args[1:], " "), api.SearchOptions{Network: network}, limit)
			})
			if err != nil {
				return err
			}
			empty := "No results."
			if args[0] != "people" || len(network) > 0 && !(len(network) == 1 && network[0] == "1st") {
				empty = "No results. " + api.SearchBoxCaveat + "."
			}
			return printList(a, results, empty, output.SearchResults)
		},
	}
	limitFlag(cmd, &limit, "results", 10, api.MaxListItems)
	cmd.Flags().StringSliceVar(&network, "network", nil, "people only: keep these connection degrees (1st, 2nd, 3rd)")
	return cmd
}

func (a *app) companyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "company",
		Short: "Look at and follow companies and schools",
		Long: "Look at and follow company and school pages.\n\n" +
			"COMPANY is the page address (linkedin.com/company/NAME or /school/NAME) or NAME.",
	}
	show := &cobra.Command{
		Use:     "show COMPANY",
		Short:   "Show a company or school page",
		Example: "  lmw company show https://www.linkedin.com/company/anthropic/",
		Args:    exactArgs(1, "one company, such as anthropic"),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := api.ParseCompany(args[0])
			if err != nil {
				return err
			}
			company, err := fetch(a, func(c *voyager.Client) (api.Company, error) { return api.GetCompany(cmd.Context(), c, name) })
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, company)
			}
			a.printf("%s\n", output.Company(company))
			return nil
		},
	}
	var limit int
	posts := &cobra.Command{
		Use:     "posts COMPANY",
		Short:   "Show the latest posts of a company or school page",
		Example: "  lmw company posts anthropic -n 5",
		Args:    exactArgs(1, "one company, such as anthropic"),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := api.ParseCompany(args[0])
			if err != nil {
				return err
			}
			found, err := fetch(a, func(c *voyager.Client) ([]api.FeedPost, error) {
				id, err := api.CompanyID(cmd.Context(), c, name)
				if err != nil {
					return nil, err
				}
				return api.CompanyPosts(cmd.Context(), c, id, limit)
			})
			if err != nil {
				return err
			}
			return a.printPosts(found, false, "No posts.")
		},
	}
	limitFlag(posts, &limit, "posts", 5, api.MaxFeedPosts)
	cmd.AddCommand(show, posts, a.followCommand("company", true), a.followCommand("company", false))
	return cmd
}

// followCommand builds `profile follow`, `company unfollow` and the like.
func (a *app) followCommand(kind string, follow bool) *cobra.Command {
	verb, word := "Follow", "follow"
	if !follow {
		verb, word = "Unfollow", "unfollow"
	}
	arg := map[string]string{"profile": "PROFILE", "company": "COMPANY"}[kind]
	return &cobra.Command{
		Use:   word + " " + arg,
		Short: verb + " a " + map[string]string{"profile": "member", "company": "company or school"}[kind] + " (asks for confirmation)",
		Args:  exactArgs(1, "one "+kind),
		RunE: func(cmd *cobra.Command, args []string) error {
			change, target, err := a.followChange(cmd.Context(), kind, args[0], follow)
			if err != nil {
				return err
			}
			if ok, err := a.approve(verb+" "+target+".", word); err != nil || !ok {
				return err
			}
			if err := a.withClient(change); err != nil {
				return err
			}
			return a.done(verb+"ed "+target+".", map[string]any{kind: target, "following": follow})
		},
	}
}

// followChange checks a follow target and returns the change to make.
func (a *app) followChange(ctx context.Context, kind, input string, follow bool) (func(*voyager.Client) error, string, error) {
	if kind == "company" {
		name, err := api.ParseCompany(input)
		if err != nil {
			return nil, "", err
		}
		return func(c *voyager.Client) error { return api.FollowCompany(ctx, c, name, follow) }, name, nil
	}
	ref, err := api.ParseProfile(input)
	if err != nil {
		return nil, "", err
	}
	return func(c *voyager.Client) error { return api.FollowMember(ctx, c, ref, follow) }, ref.String(), nil
}

func (a *app) jobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Search job postings and read them",
	}
	var limit int
	var search api.JobSearch
	searchCmd := &cobra.Command{
		Use:   "search [KEYWORDS...]",
		Short: "Search job postings",
		Example: "  lmw jobs search go developer --location Barcelona\n" +
			"  lmw jobs search \"data engineer\" --workplace remote --posted week",
		RunE: func(cmd *cobra.Command, args []string) error {
			search.Keywords = strings.Join(args, " ")
			if search.Keywords == "" && search.Location == "" {
				return errUsage("give some keywords or a --location")
			}
			jobs, err := fetch(a, func(c *voyager.Client) ([]api.Job, error) { return api.SearchJobs(cmd.Context(), c, search, limit) })
			if err != nil {
				return err
			}
			return printList(a, jobs, "No jobs found.", output.Jobs)
		},
	}
	limitFlag(searchCmd, &limit, "jobs", 10, api.MaxListItems)
	searchCmd.Flags().StringVar(&search.Location, "location", "", "city, region or country, as you would type it on the website")
	searchCmd.Flags().StringSliceVar(&search.Workplace, "workplace", nil, "onsite, remote or hybrid (comma-separated)")
	searchCmd.Flags().StringVar(&search.Posted, "posted", "", "only jobs posted in the last day, week or month")

	var full bool
	show := &cobra.Command{
		Use:     "show JOB",
		Short:   "Show a job posting",
		Example: "  lmw jobs show https://www.linkedin.com/jobs/view/4012345678/",
		Args:    exactArgs(1, "one job: its address or id"),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := api.ParseJob(args[0])
			if err != nil {
				return err
			}
			job, err := fetch(a, func(c *voyager.Client) (api.Job, error) { return api.GetJob(cmd.Context(), c, id) })
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, job)
			}
			a.printf("%s\n", output.Job(job, full))
			return nil
		},
	}
	show.Flags().BoolVar(&full, "full", false, "show the complete description")
	cmd.AddCommand(searchCmd, show, a.jobsTrackedCommand())
	return cmd
}

func (a *app) mcpDiscoverTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:  "search",
			Title: "Search LinkedIn",
			Description: "Search people, companies, schools or groups like the website's search box. Posts can't be searched; for jobs use search_jobs. " +
				"People are found by name. Companies, schools, groups and 2nd/3rd-degree filters may come back empty: " + api.SearchBoxCaveat + ".",
			ReadOnly: true,
			Params: []mcp.Param{
				{Name: "type", Type: "string", Description: "what to search", Enum: searchTypes(), Required: true},
				{Name: "keywords", Type: "string", Description: "what to look for", Required: true},
				{Name: "network", Type: "string", Description: "people only: comma-separated degrees to keep, e.g. 1st,2nd"},
				limitParam("results", 10, api.MaxListItems),
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				var network []string
				for _, d := range strings.Split(args.String("network"), ",") {
					if d = strings.TrimSpace(d); d != "" {
						network = append(network, d)
					}
				}
				items, err := fetch(a, func(c *voyager.Client) ([]api.SearchResult, error) {
					return api.Search(ctx, c, args.String("type"), args.String("keywords"), api.SearchOptions{Network: network}, args.Int("limit"))
				})
				return listOf("results", items, err)
			},
		},
		{
			Name:        "get_company",
			Title:       "Company page",
			Description: "A company or school page: tagline, description, industry, size, headquarters, followers and website.",
			ReadOnly:    true,
			Params: []mcp.Param{{Name: "company", Type: "string", Required: true,
				Description: "the page address (linkedin.com/company/NAME) or NAME"}},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				name, err := api.ParseCompany(args.String("company"))
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.Company, error) { return api.GetCompany(ctx, c, name) })
			},
		},
		{
			Name:        "get_company_posts",
			Title:       "Company posts",
			Description: "The latest posts of a company or school page, newest first.",
			ReadOnly:    true,
			Params: []mcp.Param{{Name: "company", Type: "string", Required: true, Description: "the page address (linkedin.com/company/NAME) or NAME"},
				limitParam("posts", 5, api.MaxFeedPosts)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				name, err := api.ParseCompany(args.String("company"))
				if err != nil {
					return nil, err
				}
				posts, err := fetch(a, func(c *voyager.Client) ([]api.FeedPost, error) {
					id, err := api.CompanyID(ctx, c, name)
					if err != nil {
						return nil, err
					}
					return api.CompanyPosts(ctx, c, id, args.Int("limit"))
				})
				return listOf("posts", posts, err)
			},
		},
		{
			Name:        "search_jobs",
			Title:       "Search jobs",
			Description: "Search job postings by keywords and location, optionally only remote/hybrid/on-site or recent ones.",
			ReadOnly:    true,
			Params: []mcp.Param{
				{Name: "keywords", Type: "string", Description: "job title, skills or company"},
				{Name: "location", Type: "string", Description: "city, region or country"},
				{Name: "workplace", Type: "string", Description: "comma-separated: onsite, remote, hybrid"},
				{Name: "posted", Type: "string", Description: "only jobs posted in the last day, week or month", Enum: []string{"day", "week", "month"}},
				limitParam("jobs", 10, api.MaxListItems),
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				search := api.JobSearch{Keywords: args.String("keywords"), Location: args.String("location"), Posted: args.String("posted")}
				for _, w := range strings.Split(args.String("workplace"), ",") {
					if w = strings.TrimSpace(w); w != "" {
						search.Workplace = append(search.Workplace, w)
					}
				}
				if search.Keywords == "" && search.Location == "" {
					return nil, errUsage("give keywords or a location")
				}
				items, err := fetch(a, func(c *voyager.Client) ([]api.Job, error) { return api.SearchJobs(ctx, c, search, args.Int("limit")) })
				return listOf("jobs", items, err)
			},
		},
		{
			Name:        "get_job",
			Title:       "Job posting",
			Description: "A job posting with its description, company, location, workplace, applicants and apply link.",
			ReadOnly:    true,
			Params:      []mcp.Param{{Name: "job", Type: "string", Required: true, Description: "the job address (linkedin.com/jobs/view/ID) or id"}},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				id, err := api.ParseJob(args.String("job"))
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.Job, error) { return api.GetJob(ctx, c, id) })
			},
		},
		{
			Name:        "set_following",
			Title:       "Follow or unfollow",
			Description: "Follow or unfollow a member or a company/school page.",
			Params: []mcp.Param{
				{Name: "kind", Type: "string", Description: "what to follow", Enum: []string{"profile", "company"}, Required: true},
				{Name: "target", Type: "string", Required: true, Description: "the profile or company page address, or its name"},
				{Name: "follow", Type: "boolean", Required: true, Description: "true to follow, false to unfollow"},
			},
			Confirm: func(args mcp.Args) (string, error) {
				verb := "Unfollow"
				if args.Bool("follow") {
					verb = "Follow"
				}
				return verb + " " + args.String("target") + " on LinkedIn?", nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				change, target, err := a.followChange(ctx, args.String("kind"), args.String("target"), args.Bool("follow"))
				if err != nil {
					return nil, err
				}
				if err := a.withClient(change); err != nil {
					return nil, err
				}
				return map[string]any{args.String("kind"): target, "following": args.Bool("follow")}, nil
			},
		},
	}
}
