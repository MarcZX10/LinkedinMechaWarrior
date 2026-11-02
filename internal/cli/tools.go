package cli

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/har"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/identity"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

func (a *app) apiCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Send raw requests to LinkedIn's internal API",
	}
	var params []string
	get := &cobra.Command{
		Use:   "get PATH",
		Short: "GET an internal API path and print the JSON",
		Example: "  lmw api get /me\n" +
			"  lmw api get /feed/updatesV2 -p count=10 -p q=chronFeed -p start=0",
		Args: exactArgs(1, "one API path, such as /me"),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := parseParams(params)
			if err != nil {
				return err
			}
			var raw []byte
			err = a.withClient(func(c *voyager.Client) (err error) {
				raw, err = c.Get(cmd.Context(), args[0], query)
				return err
			})
			if err != nil {
				return err
			}
			return printRawJSON(a, raw)
		},
	}
	get.Flags().StringArrayVarP(&params, "param", "p", nil, "query parameter as KEY=VALUE (repeatable)")
	cmd.AddCommand(get)
	return cmd
}

// parseParams reads KEY=VALUE query parameters, keeping their order.
func parseParams(items []string) (voyager.Params, error) {
	var query voyager.Params
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			return nil, errs.New(errs.Usage, "query parameters must look like KEY=VALUE, got %q", item)
		}
		query = append(query, voyager.Param{Key: key, Value: value})
	}
	return query, nil
}

// parseQuery reads a query string such as "count=10&q=chronFeed". Values are
// passed on as written, so they may already be encoded.
func parseQuery(query string) (voyager.Params, error) {
	query = strings.TrimPrefix(strings.TrimSpace(query), "?")
	if query == "" {
		return nil, nil
	}
	return parseParams(strings.Split(query, "&"))
}

func printRawJSON(a *app, raw []byte) error {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		a.printf("%s\n", raw)
		return nil
	}
	a.printf("%s\n", pretty.String())
	return nil
}

func (a *app) harCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "har",
		Short: "Inspect HAR files recorded in your browser",
		Long: `Inspect HAR files recorded in your browser's developer tools.

A HAR shows which internal API calls LinkedIn's web app makes, which is how
commands are built and fixed when LinkedIn changes something. To record one,
open DevTools > Network in your browser, go to the LinkedIn page you are
interested in, and save the requests as a HAR file (right-click the list).

The file contains private data such as messages; don't share it as-is.`,
		Example: `  # List the API calls the web app made
  lmw har inspect linkedin.har

  # Only calls whose URL contains "graphql"
  lmw har inspect linkedin.har --filter graphql

  # Print the request and response of entry 42
  lmw har inspect linkedin.har --show 42`,
	}
	var filter string
	show := -1
	inspect := &cobra.Command{
		Use:   "inspect FILE",
		Short: "List the LinkedIn API calls in a HAR file, or show one",
		Args:  exactArgs(1, "one HAR file"),
		RunE: func(_ *cobra.Command, args []string) error {
			file, err := har.Load(args[0])
			if err != nil {
				return err
			}
			if show >= 0 {
				return a.showHAREntry(file, show)
			}
			calls := file.Calls(filter)
			if a.jsonOut {
				if calls == nil {
					calls = []har.Call{}
				}
				return output.JSON(a.stdout, calls)
			}
			if len(calls) == 0 {
				a.printf("No LinkedIn API calls found. Record the HAR while LinkedIn is loading.\n")
				return nil
			}
			for _, call := range calls {
				a.printf("%s\n", call)
			}
			a.printf("\n%d API calls. Show one with --show INDEX.\n", len(calls))
			return nil
		},
	}
	inspect.Flags().StringVar(&filter, "filter", "", "only calls whose URL contains this text")
	inspect.Flags().IntVar(&show, "show", -1, "print the request and response of the entry with this index")
	cmd.AddCommand(inspect, a.harLearnCommand())
	return cmd
}

func (a *app) showHAREntry(file *har.File, index int) error {
	entry, err := file.Entry(index)
	if err != nil {
		return err
	}
	a.printf("%s %s\nStatus: %d\n", entry.Request.Method, entry.Request.URL, entry.Response.Status)
	if entry.Request.PostData != nil && entry.Request.PostData.Text != "" {
		a.printf("\nRequest body:\n")
		_ = printRawJSON(a, []byte(entry.Request.PostData.Text))
	}
	body, err := entry.ResponseBody()
	if err != nil {
		return errs.Wrap(errs.Usage, err, "cannot decode the response body: %v", err)
	}
	a.printf("\nResponse body:\n")
	if len(body) == 0 {
		a.printf("(empty; some browsers leave bodies out of the HAR)\n")
		return nil
	}
	return printRawJSON(a, body)
}

func (a *app) identityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Show or change which browser lmw presents itself as",
		Long: "lmw's requests carry the TLS fingerprint and headers of a desktop browser.\n" +
			"By default that is a current Chrome on this operating system; importing a HAR\n" +
			"recorded in your own browser copies its exact headers instead.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show the current identity",
			Args:  exactArgs(0, "no arguments"),
			RunE: func(_ *cobra.Command, _ []string) error {
				id, err := identity.Load(a.cfg.IdentityFile())
				if err != nil {
					return errs.Wrap(errs.Storage, err, "%v", err)
				}
				if a.jsonOut {
					return output.JSON(a.stdout, id)
				}
				family, major := id.Browser()
				a.printf("Source:          %s\nBrowser:         %s %d (TLS profile %s)\nUser-Agent:      %s\n",
					id.Source, family, major, profileLabel(id), id.UserAgent)
				if id.SecCHUA != "" {
					a.printf("sec-ch-ua:       %s\nPlatform:        %s\n", id.SecCHUA, id.SecCHUAPlatform)
				}
				a.printf("Accept-Language: %s\n", id.AcceptLanguage)
				if id.LiTrack != "" {
					a.printf("x-li-track:      %s\n", id.LiTrack)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "import FILE",
			Short: "Copy the identity of the browser that recorded a HAR file",
			Args:  exactArgs(1, "one HAR file"),
			RunE: func(_ *cobra.Command, args []string) error {
				file, err := har.Load(args[0])
				if err != nil {
					return err
				}
				id, err := file.Identity()
				if err != nil {
					return err
				}
				if err := identity.Save(a.cfg.IdentityFile(), id); err != nil {
					return errs.Wrap(errs.Storage, err, "cannot save the identity: %v", err)
				}
				a.printf("Identity copied: %s\n", id.UserAgent)
				return nil
			},
		},
		&cobra.Command{
			Use:   "reset",
			Short: "Go back to the default identity",
			Args:  exactArgs(0, "no arguments"),
			RunE: func(_ *cobra.Command, _ []string) error {
				if err := identity.Reset(a.cfg.IdentityFile()); err != nil {
					return errs.Wrap(errs.Storage, err, "%v", err)
				}
				a.printf("Using the default identity.\n")
				return nil
			},
		},
	)
	return cmd
}

func profileLabel(id identity.Identity) string {
	hello := voyager.ProfileFor(id).GetClientHelloId()
	return hello.Str()
}

func (a *app) limitsCommand() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "limits",
		Short: "Show request budgets and cooldowns",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			pacer := a.newPacer(a.cfg)
			if clear {
				if err := pacer.ClearCooldown(); err != nil {
					return err
				}
				a.note("Cooldown cleared.\n")
			}
			usage := pacer.Usage()
			if a.jsonOut {
				return output.JSON(a.stdout, usage)
			}
			a.printf("%s\n", usage)
			return nil
		},
	}
	cmd.Flags().BoolVar(&clear, "clear-cooldown", false, "lift a cooldown after checking the account in your browser")
	return cmd
}
