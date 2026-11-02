package cli

import (
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
	var online bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that lmw is ready to fly",
		Long: "Check the setup: the stored session, the browser identity, request budgets\n" +
			"and cooldowns, and the GraphQL query versions learned from HAR files.\n" +
			"Sends nothing to LinkedIn unless --online is given, which checks the session\n" +
			"with one request.",
		Example: "  lmw doctor\n  lmw doctor --online",
		Args:    exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := a.runChecks(cmd, online)
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
