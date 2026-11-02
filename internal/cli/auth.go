package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/har"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/session"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

func (a *app) authCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Import, check or remove the LinkedIn session",
	}
	cmd.AddCommand(a.authImportCommand(), a.authStatusCommand(), a.authLogoutCommand())
	return cmd
}

func (a *app) authImportCommand() *cobra.Command {
	var browser, harFile string
	var noVerify bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Reuse the LinkedIn session from your browser",
		Long: `Import the LinkedIn session from the browser you normally use.

lmw never logs in by itself: log in to LinkedIn in your browser as usual
(including 2FA), then import that session from a HAR file (recommended, it
also copies your browser's identity), from the browser's cookie store, or by
pasting the li_at and JSESSIONID cookies.

To record a HAR in Chrome or Edge: open DevTools > Network, reload LinkedIn,
right-click the request list and choose "Save all as HAR (with sensitive data)".
In Firefox: Network > gear icon > "Save All As HAR".

The session is checked with one API request and stored in the system keyring.`,
		Example: `  # From a HAR file recorded in your browser (recommended)
  lmw auth import --har linkedin.har

  # From the cookie store of an installed browser
  lmw auth import --browser
  lmw auth import --browser firefox

  # By pasting the cookies (DevTools > Application > Cookies)
  lmw auth import`,
		Args: func(_ *cobra.Command, args []string) error {
			// "--browser firefox" and "--har FILE" reach us as the flag (optional value)
			// plus an argument.
			if len(args) > 1 || (len(args) == 1 && browser != "any" && harFile != autoHAR) {
				return errs.New(errs.Usage, "unexpected argument %q; use --browser NAME, --har FILE or no flags", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if browser == "any" {
					browser = args[0]
				} else {
					harFile = args[0]
				}
			}
			if browser != "" && harFile != "" {
				return errs.New(errs.Usage, "use either --browser or --har, not both")
			}
			cookies, source, err := a.collectCookies(cmd, browser, harFile)
			if err != nil {
				return err
			}
			s, err := session.New(cookies, source)
			if err != nil {
				return err
			}
			if !noVerify {
				var profile api.Profile
				client, err := a.client(s)
				if err != nil {
					return err
				}
				if profile, err = api.Me(cmd.Context(), client); err != nil {
					return fmt.Errorf("the session was not saved: %w", err)
				}
				s = client.Session()
				a.printf("Logged in as %s.\n", profile.Name)
			}
			where, err := a.newStore(a.cfg).Save(s)
			if err != nil {
				return err
			}
			a.printf("Session from %s saved in %s.\n", source, where)
			return nil
		},
	}
	cmd.Flags().StringVar(&browser, "browser", "", "read cookies from an installed browser (optionally by name: chrome, firefox, edge...)")
	cmd.Flags().Lookup("browser").NoOptDefVal = "any"
	cmd.Flags().StringVar(&harFile, "har", "", "read cookies and browser identity from a HAR file (without a file: find the newest, asking first)")
	cmd.Flags().Lookup("har").NoOptDefVal = autoHAR
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "save without checking the session with LinkedIn")
	return cmd
}

func (a *app) collectCookies(cmd *cobra.Command, browser, harFile string) (map[string]string, string, error) {
	switch {
	case harFile != "":
		path, err := a.resolveHAR(harFile)
		if err != nil {
			return nil, "", err
		}
		file, err := har.Load(path)
		if err != nil {
			return nil, "", err
		}
		cookies, err := file.Cookies()
		if err != nil {
			return nil, "", err
		}
		id, err := file.Identity()
		if err != nil {
			// The session is still usable; only the identity stays as it was.
			a.printf("Browser identity not copied: %s\n", err)
		} else {
			if err := identity.Save(a.cfg.IdentityFile(), id); err != nil {
				return nil, "", errs.Wrap(errs.Storage, err, "cannot save the browser identity: %v", err)
			}
			a.printf("Browser identity copied from the HAR file: %s\n", id.UserAgent)
		}
		if learned, err := a.learnQueries(file); err != nil {
			slog.Warn("could not learn the GraphQL queries of the HAR file", "error", err)
		} else if len(learned) > 0 {
			a.printf("Learned %d GraphQL query versions from the HAR file.\n", len(learned))
		}
		return cookies, "HAR file", nil

	case browser != "":
		name := browser
		if name == "any" {
			name = ""
		}
		candidates, err := a.findSessions(cmd.Context(), name)
		if err != nil {
			return nil, "", err
		}
		if len(candidates) > 1 {
			var labels []string
			for _, c := range candidates {
				labels = append(labels, c.Label())
			}
			a.printf("Found LinkedIn sessions in: %s. Using the most recent one.\n", strings.Join(labels, ", "))
		}
		return candidates[0].Cookies, candidates[0].Label(), nil

	default:
		fmt.Fprintln(a.stderr, "Copy the cookies from your browser: DevTools > Application (Storage in Firefox) > Cookies > https://www.linkedin.com")
		liAt, err := a.promptSecret("li_at: ")
		if err != nil {
			return nil, "", err
		}
		jsessionID, err := a.prompt("JSESSIONID: ")
		if err != nil {
			return nil, "", err
		}
		if !strings.HasPrefix(jsessionID, `"`) {
			// Browsers store it quoted, and LinkedIn expects it that way.
			jsessionID = `"` + jsessionID + `"`
		}
		return map[string]string{"li_at": liAt, "JSESSIONID": jsessionID}, "pasted cookies", nil
	}
}

func (a *app) authStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check that the session works (one API request)",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			var profile api.Profile
			var source string
			err := a.withClient(func(c *voyager.Client) error {
				source = c.Session().Describe()
				var err error
				profile, err = api.Me(cmd.Context(), c)
				return err
			})
			var e *errs.Error
			if errors.As(err, &e) && e.Kind == errs.AuthRequired {
				if a.jsonOut {
					_ = output.JSON(a.stdout, map[string]any{"authenticated": false, "reason": e.Error()})
				}
				return err
			}
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, map[string]any{"authenticated": true, "profile": profile, "session": source})
			}
			a.printf("Logged in as %s", profile.Name)
			if profile.URL != "" {
				a.printf(" (%s)", profile.URL)
			}
			a.printf("\nSession %s.\n", source)
			return nil
		},
	}
}

func (a *app) authLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored session from this computer",
		Long: "Remove the stored session from this computer.\n\n" +
			"The session itself stays valid on LinkedIn until you log out in your browser.",
		Args: exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			ok, err := a.confirm("Type 'logout' to remove the LinkedIn session from this computer: ", "logout")
			if err != nil || !ok {
				a.printf("Cancelled.\n")
				return err
			}
			if err := a.newStore(a.cfg).Delete(); err != nil {
				return err
			}
			a.printf("Session removed. It stays valid on LinkedIn until you log out in your browser.\n")
			return nil
		},
	}
}
