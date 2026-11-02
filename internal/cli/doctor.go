package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/version"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// check is one line of `lmw doctor`.
type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func (a *app) doctorCommand() *cobra.Command {
	var online, probe bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that lmw is ready to fly",
		Long: "Check the setup: the stored session, the browser identity, request budgets\n" +
			"and cooldowns, and the GraphQL query versions learned from HAR files.\n" +
			"Sends nothing to LinkedIn unless --online is given, which checks the session\n" +
			"with one request.\n\n" +
			"--probe also reads a little of every feature (feed, profile, notifications,\n" +
			"invitations, connections, messages, search, saved posts, jobs, companies) to\n" +
			"show which ones work with your account today. That is about a dozen paced\n" +
			"requests, a minute or so, and it stops at the first sign of pushback.",
		Example: "  lmw doctor\n  lmw doctor --online\n  lmw doctor --probe",
		Args:    exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := a.runChecks(cmd, online && !probe)
			if probe {
				checks = append(checks, a.runProbes(cmd.Context())...)
			}
			failed := 0
			for _, c := range checks {
				if !c.OK {
					failed++
				}
			}
			if a.jsonOut {
				if err := output.JSON(a.stdout, checks); err != nil {
					return err
				}
			} else {
				a.printf("lmw %s · %s/%s\n\n", version.Version, runtime.GOOS, runtime.GOARCH)
				for _, c := range checks {
					mark := "✔"
					if !c.OK {
						mark = "✘"
					}
					a.printf("%s %-14s %s\n", mark, c.Name, c.Detail)
					if c.Fix != "" {
						a.printf("  %-14s → %s\n", "", c.Fix)
					}
				}
			}
			if failed > 0 {
				return errs.New(errs.AuthRequired, "%d of %d checks failed", failed, len(checks))
			}
			if !a.jsonOut {
				a.printf("\nAll systems go.\n")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&online, "online", false, "also check the session with LinkedIn (one request)")
	cmd.Flags().BoolVar(&probe, "probe", false, "also try a small read of every feature (about a dozen requests)")
	return cmd
}

func (a *app) runChecks(cmd *cobra.Command, online bool) []check {
	checks := []check{{Name: "state", OK: true, Detail: a.cfg.StateDir}}

	stored, err := a.newStore(a.cfg).Load()
	if err != nil {
		checks = append(checks, check{Name: "session", Detail: err.Error(), Fix: "lmw auth import --har FILE"})
	} else {
		checks = append(checks, check{Name: "session", OK: true, Detail: stored.Describe()})
	}

	if id, err := identity.Load(a.cfg.IdentityFile()); err != nil {
		checks = append(checks, check{Name: "identity", Detail: err.Error(), Fix: "lmw identity reset"})
	} else {
		family, major := id.Browser()
		checks = append(checks, check{Name: "identity", OK: true,
			Detail: fmt.Sprintf("%s %d, TLS profile %s, from %s", family, major, profileLabel(id), id.Source)})
	}

	usage := a.newPacer(a.cfg).Usage()
	budget := check{Name: "budget", OK: usage.LastHour < usage.HourlyBudget && usage.LastDay < usage.DailyBudget,
		Detail: fmt.Sprintf("%d/%d requests in the last hour, %d/%d in 24h", usage.LastHour, usage.HourlyBudget, usage.LastDay, usage.DailyBudget)}
	if !budget.OK {
		budget.Fix = "wait; `lmw limits` shows the usage"
	}
	checks = append(checks, budget)
	if usage.CooldownUntil != nil {
		checks = append(checks, check{Name: "cooldown", Detail: fmt.Sprintf("until %s: %s", usage.CooldownUntil.Format("2006-01-02 15:04"), usage.CooldownReason),
			Fix: "check the account in your browser, then `lmw limits --clear-cooldown`"})
	} else {
		checks = append(checks, check{Name: "cooldown", OK: true, Detail: "none"})
	}

	learned, err := loadLearnedQueries(a.cfg.QueriesFile())
	switch {
	case err != nil:
		checks = append(checks, check{Name: "queries", Detail: err.Error()})
	case len(learned) == 0:
		checks = append(checks, check{Name: "queries", OK: true, Detail: fmt.Sprintf("%d built-in GraphQL queries, none learned yet", len(api.Queries()))})
	default:
		checks = append(checks, check{Name: "queries", OK: true, Detail: fmt.Sprintf("%d versions learned from HAR files", len(learned))})
	}

	if online && stored != nil {
		profile, err := fetch(a, func(c *voyager.Client) (api.Profile, error) { return api.Me(cmd.Context(), c) })
		if err != nil {
			checks = append(checks, check{Name: "linkedin", Detail: err.Error(), Fix: "see `lmw auth --help`"})
		} else {
			checks = append(checks, check{Name: "linkedin", OK: true, Detail: "logged in as " + profile.Name})
		}
	}
	return checks
}

// probe is one live read of `lmw doctor --probe`.
type probe struct {
	name string
	run  func(ctx context.Context, c *voyager.Client, me api.Profile) (string, error)
}

func countOf[T any](items []T, err error) (string, error) {
	return fmt.Sprintf("read %d", len(items)), err
}

var probes = []probe{
	{"feed", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.Feed(ctx, c, 1))
	}},
	{"profile", func(ctx context.Context, c *voyager.Client, me api.Profile) (string, error) {
		p, err := api.GetProfile(ctx, c, api.ProfileRef{PublicID: me.PublicID})
		return fmt.Sprintf("%d positions, %d skills", len(p.Experience), len(p.Skills)), err
	}},
	{"notifications", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.Notifications(ctx, c, 1))
	}},
	{"invitations", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.Invitations(ctx, c, 1))
	}},
	{"connections", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.Connections(ctx, c, 1))
	}},
	{"messages", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.Conversations(ctx, c, "inbox", 1))
	}},
	{"search", func(ctx context.Context, c *voyager.Client, me api.Profile) (string, error) {
		return countOf(api.Search(ctx, c, "people", me.Name, api.SearchOptions{}, 1))
	}},
	{"saved", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.SavedPosts(ctx, c, 1))
	}},
	{"jobs", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		return countOf(api.SearchJobs(ctx, c, api.JobSearch{Keywords: "engineer"}, 1))
	}},
	{"company", func(ctx context.Context, c *voyager.Client, _ api.Profile) (string, error) {
		company, err := api.GetCompany(ctx, c, "linkedin")
		return company.Name, err
	}},
}

// stopsProbing reports errors after which no more requests should be sent.
func stopsProbing(err error) bool {
	for _, kind := range []errs.Kind{errs.AuthRequired, errs.Challenge, errs.RateLimited, errs.Budget, errs.Cooldown} {
		if voyager.IsKind(err, kind) {
			return true
		}
	}
	return false
}

// runProbes reads a little of everything, so a person can see which features
// work with their account today. It stops at the first sign of pushback.
func (a *app) runProbes(ctx context.Context) []check {
	var checks []check
	err := a.withClient(func(c *voyager.Client) error {
		me, err := api.Me(ctx, c)
		if err != nil {
			checks = append(checks, check{Name: "probe me", Detail: err.Error(), Fix: "lmw auth import --har FILE"})
			return nil
		}
		checks = append(checks, check{Name: "probe me", OK: true, Detail: me.Name})
		for i, p := range probes {
			fmt.Fprintf(a.stderr, "probing %s (%d/%d)...\n", p.name, i+1, len(probes))
			detail, err := p.run(ctx, c, me)
			if err != nil {
				fix := "record a HAR on the matching page and run `lmw har learn FILE`"
				if stopsProbing(err) {
					fix = "stopped probing; see `lmw doctor` and Troubleshooting"
				}
				checks = append(checks, check{Name: "probe " + p.name, Detail: err.Error(), Fix: fix})
				if stopsProbing(err) || ctx.Err() != nil {
					return nil
				}
				continue
			}
			checks = append(checks, check{Name: "probe " + p.name, OK: true, Detail: detail})
		}
		return nil
	})
	if err != nil {
		checks = append(checks, check{Name: "probe", Detail: err.Error()})
	}
	return checks
}
