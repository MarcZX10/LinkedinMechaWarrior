// Package cli implements the lmw command line.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/auth"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/config"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/pacing"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/session"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/version"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// app holds what commands share. The function fields are replaced in tests.
type app struct {
	stdin   *bufio.Reader
	stdinFD int // -1 when stdin is not a terminal file
	stdout  io.Writer
	stderr  io.Writer

	jsonOut bool
	verbose bool
	cfg     config.Config

	loadConfig   func() (config.Config, error)
	newDoer      func(identity.Identity, time.Duration) (voyager.Doer, error)
	newPacer     func(config.Config) *pacing.Pacer
	newStore     func(config.Config) session.Store
	findSessions func(context.Context, string) ([]auth.Candidate, error)
}

// Main runs lmw with the process arguments and returns the exit code.
func Main() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a := &app{
		stdin:      bufio.NewReader(os.Stdin),
		stdinFD:    int(os.Stdin.Fd()),
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		loadConfig: config.Load,
		newDoer:    voyager.NewHTTPDoer,
		newPacer: func(cfg config.Config) *pacing.Pacer {
			p := defaultPacer(cfg)
			p.Sleep = interruptibleSleep(ctx)
			return p
		},
		newStore:     func(cfg config.Config) session.Store { return session.NewStore(cfg.SessionStore, cfg.SessionFile()) },
		findSessions: auth.FindSessions,
	}
	return a.run(ctx, os.Args[1:])
}

func defaultPacer(cfg config.Config) *pacing.Pacer {
	return pacing.New(cfg.RequestLogFile(), cfg.RequestMinDelay, cfg.RequestMaxDelay, cfg.HourlyRequestBudget, cfg.DailyRequestBudget)
}

// interruptibleSleep lets Ctrl-C end a pacing pause; the request that would
// follow then fails with the cancelled context.
func interruptibleSleep(ctx context.Context) func(time.Duration) {
	return func(d time.Duration) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	}
}

func (a *app) run(ctx context.Context, args []string) int {
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetIn(a.stdin)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)

	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return 0
	case ctx.Err() != nil:
		fmt.Fprintln(a.stderr, "lmw: interrupted")
		return 130
	}
	fmt.Fprintf(a.stderr, "lmw: %s\n", err)
	var e *errs.Error
	if errors.As(err, &e) && e.Kind == errs.Usage {
		return 2
	}
	return 1
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "lmw",
		Short: "A command-line client for LinkedIn",
		Long: "lmw (LinkedinMechaWarrior) is a command-line client for your own LinkedIn account.\n\n" +
			"It talks to LinkedIn's internal web API with plain HTTP requests that look like\n" +
			"your browser's, keeps usage light with randomized pacing and request budgets,\n" +
			"and stops at the first sign of pushback from LinkedIn.\n\n" +
			"Start with `lmw auth import`.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			level := slog.LevelWarn
			if a.verbose {
				level = slog.LevelDebug
			}
			options := &slog.HandlerOptions{Level: level}
			if !a.verbose {
				// Timestamps only help when debugging.
				options.ReplaceAttr = func(groups []string, attr slog.Attr) slog.Attr {
					if len(groups) == 0 && attr.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return attr
				}
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(a.stderr, options)))
			cfg, err := a.loadConfig()
			if err != nil {
				return errs.Wrap(errs.Usage, err, "%v", err)
			}
			a.cfg = cfg
			if err := cfg.EnsureStateDir(); err != nil {
				return errs.Wrap(errs.Storage, err, "cannot create %s: %v", cfg.StateDir, err)
			}
			queries, err := loadLearnedQueries(cfg.QueriesFile())
			if err != nil {
				slog.Warn("ignoring the learned GraphQL queries", "error", err)
			}
			api.SetLearnedQueries(queries)
			return nil
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return errs.Wrap(errs.Usage, err, "%v", err) })
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, "print JSON output")
	root.PersistentFlags().BoolVarP(&a.verbose, "verbose", "v", false, "show debug logs, including every API request")
	root.SetVersionTemplate("lmw {{.Version}}\n")

	root.AddCommand(a.authCommand(), a.meCommand(), a.feedCommand(), a.profileCommand(), a.postCommand(),
		a.messagesCommand(), a.notificationsCommand(), a.invitationsCommand(), a.connectCommand(), a.connectionsCommand(),
		a.searchCommand(), a.companyCommand(), a.jobsCommand(),
		a.apiCommand(), a.harCommand(), a.identityCommand(), a.limitsCommand(), a.mcpCommand())
	return root
}

// withClient loads the session, runs fn with a client, and saves any cookie
// LinkedIn updated during the run.
func (a *app) withClient(fn func(*voyager.Client) error) error {
	store := a.newStore(a.cfg)
	s, err := store.Load()
	if err != nil {
		return err
	}
	client, err := a.client(s)
	if err != nil {
		return err
	}
	runErr := fn(client)
	if client.SessionChanged() {
		if client.Session().Validate() != nil {
			// LinkedIn ended the session; keeping it would only cause more failed requests.
			err = store.Delete()
		} else {
			_, err = store.Save(client.Session())
		}
		if err != nil {
			slog.Warn("could not update the stored session", "error", err)
		}
	}
	return runErr
}

func (a *app) client(s *session.Session) (*voyager.Client, error) {
	id, err := identity.Load(a.cfg.IdentityFile())
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "%v (reset it with `lmw identity reset`)", err)
	}
	doer, err := a.newDoer(id, a.cfg.RequestTimeout)
	if err != nil {
		return nil, err
	}
	return voyager.New(doer, a.newPacer(a.cfg), id, s), nil
}

func (a *app) printf(format string, args ...any) {
	fmt.Fprintf(a.stdout, format, args...)
}

// note prints a message for people. With --json it goes to stderr, so that
// stdout only carries the JSON.
func (a *app) note(format string, args ...any) {
	w := a.stdout
	if a.jsonOut {
		w = a.stderr
	}
	fmt.Fprintf(w, format, args...)
}

// prompt reads one line from stdin.
func (a *app) prompt(text string) (string, error) {
	fmt.Fprint(a.stderr, text)
	line, err := a.stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", errs.Wrap(errs.Usage, err, "no input")
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads a line without echoing it when stdin is a terminal.
func (a *app) promptSecret(text string) (string, error) {
	if a.stdinFD < 0 || !term.IsTerminal(a.stdinFD) {
		return a.prompt(text)
	}
	fmt.Fprint(a.stderr, text)
	raw, err := term.ReadPassword(a.stdinFD)
	fmt.Fprintln(a.stderr)
	if err != nil {
		return "", errs.Wrap(errs.Usage, err, "cannot read input: %v", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// confirm asks the user to type a word to go ahead.
func (a *app) confirm(text, word string) (bool, error) {
	answer, err := a.prompt(text)
	return answer == word, err
}

func exactArgs(n int, what string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return errs.New(errs.Usage, "expected %s", what)
		}
		return nil
	}
}

func errUsage(format string, args ...any) error {
	return errs.New(errs.Usage, format, args...)
}
