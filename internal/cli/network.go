package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

// printList prints items as JSON or for people, with a message when there are none.
func printList[T any](a *app, items []T, empty string, format func([]T) string) error {
	if a.jsonOut {
		if items == nil {
			items = []T{}
		}
		return output.JSON(a.stdout, items)
	}
	if len(items) == 0 {
		a.printf("%s\n", empty)
		return nil
	}
	a.printf("%s\n", format(items))
	return nil
}

func limitFlag(cmd *cobra.Command, limit *int, what string, fallback, max int) {
	cmd.Flags().IntVarP(limit, "limit", "n", fallback, fmt.Sprintf("number of %s (1-%d)", what, max))
}

// done reports a change made on LinkedIn, as JSON or for people.
func (a *app) done(message string, result map[string]any) error {
	if a.jsonOut {
		return output.JSON(a.stdout, result)
	}
	a.printf("%s\n", message)
	return nil
}

// approve shows what a command will change and asks the user to type word.
func (a *app) approve(what, word string) (bool, error) {
	a.note("%s\n", what)
	ok, err := a.confirm(fmt.Sprintf("Type '%s' to go ahead: ", word), word)
	if err != nil || !ok {
		a.note("Cancelled; nothing was changed on LinkedIn.\n")
	}
	return ok, err
}

func (a *app) invitationsCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "invitations",
		Short: "List and answer connection requests",
		Long: "List the connection requests you received, newest first. The subcommands\n" +
			"list the ones you sent, and accept, ignore or withdraw them by id.",
		Example: "  lmw invitations\n" +
			"  lmw invitations accept 7123456789012345678\n" +
			"  lmw invitations sent",
		Args: exactArgs(0, "no arguments, or a subcommand such as accept"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			invitations, err := fetch(a, func(c *voyager.Client) ([]api.Invitation, error) { return api.Invitations(cmd.Context(), c, limit) })
			if err != nil {
				return err
			}
			return printList(a, invitations, "No pending invitations.", output.Invitations)
		},
	}
	limitFlag(cmd, &limit, "invitations", 10, api.MaxListItems)

	var sentLimit int
	sent := &cobra.Command{
		Use:   "sent",
		Short: "List the connection requests you sent that are still pending",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			invitations, err := fetch(a, func(c *voyager.Client) ([]api.Invitation, error) {
				return api.SentInvitations(cmd.Context(), c, sentLimit)
			})
			if err != nil {
				return err
			}
			return printList(a, invitations, "No pending sent invitations.", output.Invitations)
		},
	}
	limitFlag(sent, &sentLimit, "invitations", 10, api.MaxListItems)

	withdraw := &cobra.Command{
		Use:   "withdraw ID",
		Short: "Withdraw a connection request you sent (asks for confirmation)",
		Args:  exactArgs(1, "one invitation id, as shown by `lmw invitations sent`"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ok, err := a.approve("Withdraw your invitation "+args[0]+".", "withdraw"); err != nil || !ok {
				return err
			}
			err := a.withClient(func(c *voyager.Client) error { return api.WithdrawInvitation(cmd.Context(), c, args[0]) })
			if err != nil {
				return err
			}
			return a.done("Invitation withdrawn.", map[string]any{"id": args[0], "withdrawn": true})
		},
	}
	cmd.AddCommand(sent, a.invitationAnswerCommand(api.Accept), a.invitationAnswerCommand(api.Ignore), withdraw)
	return cmd
}

func (a *app) invitationAnswerCommand(action api.InvitationAction) *cobra.Command {
	verb := map[api.InvitationAction]string{api.Accept: "Accept", api.Ignore: "Ignore"}[action]
	past := map[api.InvitationAction]string{api.Accept: "Accepted", api.Ignore: "Ignored"}[action]
	return &cobra.Command{
		Use:   string(action) + " ID",
		Short: verb + " a connection request (asks for confirmation)",
		Args:  exactArgs(1, "one invitation id, as shown by `lmw invitations`"),
		RunE: func(cmd *cobra.Command, args []string) error {
			var inv api.Invitation
			err := a.withClient(func(c *voyager.Client) error {
				var err error
				if inv, err = api.FindInvitation(cmd.Context(), c, args[0]); err != nil {
					return err
				}
				what := fmt.Sprintf("%s the invitation from %s.", verb, firstNonEmptyString(inv.Name, "an unknown member"))
				if ok, err := a.approve(what, string(action)); err != nil || !ok {
					inv.ID = ""
					return err
				}
				return api.RespondToInvitation(cmd.Context(), c, inv, action)
			})
			if err != nil || inv.ID == "" {
				return err
			}
			return a.done(fmt.Sprintf("%s the invitation from %s.", past, inv.Name),
				map[string]any{"id": inv.ID, "action": string(action), "name": inv.Name})
		},
	}
}

func (a *app) connectCommand() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "connect PROFILE",
		Short: "Send a connection request (asks for confirmation)",
		Example: "  lmw connect ada-lovelace\n" +
			"  lmw connect https://www.linkedin.com/in/ada-lovelace/ --note \"Loved your talk on engines.\"",
		Args: exactArgs(1, "one profile, such as ada-lovelace"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := api.ParseProfile(args[0])
			if err != nil {
				return err
			}
			if n := len([]rune(note)); n > api.MaxInvitationNote {
				return errs.New(errs.Validation, "the note is %d characters long; LinkedIn allows %d", n, api.MaxInvitationNote)
			}
			what := "Send a connection request to " + ref.String()
			if note != "" {
				what += " with this note:\n" + note
			}
			if ok, err := a.approve(what+".", "connect"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.Connect(cmd.Context(), c, ref, note) }); err != nil {
				return err
			}
			return a.done("Connection request sent to "+ref.String()+".", map[string]any{"profile": ref.String(), "sent": true})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", fmt.Sprintf("a personal note (up to %d characters)", api.MaxInvitationNote))
	return cmd
}

func (a *app) connectionsCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "connections",
		Short: "List your connections, most recently added first",
		Args:  exactArgs(0, "no arguments, or the subcommand remove"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			connections, err := fetch(a, func(c *voyager.Client) ([]api.Connection, error) { return api.Connections(cmd.Context(), c, limit) })
			if err != nil {
				return err
			}
			return printList(a, connections, "No connections found.", output.Connections)
		},
	}
	limitFlag(cmd, &limit, "connections", 20, api.MaxListItems)
	cmd.AddCommand(&cobra.Command{
		Use:   "remove PROFILE",
		Short: "Remove a member from your connections (asks for confirmation)",
		Args:  exactArgs(1, "one profile, such as ada-lovelace"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := api.ParseProfile(args[0])
			if err != nil {
				return err
			}
			if ref.PublicID == "" {
				return errs.New(errs.Usage, "give the profile address or name (linkedin.com/in/NAME), not a member id")
			}
			if ok, err := a.approve("Remove "+ref.String()+" from your connections.", "remove"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.RemoveConnection(cmd.Context(), c, ref) }); err != nil {
				return err
			}
			return a.done(ref.String()+" is no longer a connection.", map[string]any{"profile": ref.String(), "removed": true})
		},
	})
	return cmd
}

func (a *app) notificationsCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Show your latest notifications",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			notifications, err := fetch(a, func(c *voyager.Client) ([]api.Notification, error) {
				return api.Notifications(cmd.Context(), c, limit)
			})
			if err != nil {
				return err
			}
			return printList(a, notifications, "No notifications.", output.Notifications)
		},
	}
	limitFlag(cmd, &limit, "notifications", 10, api.MaxListItems)
	return cmd
}

func (a *app) unreadCommand() *cobra.Command {
	var folders bool
	var every time.Duration
	var onChange string
	cmd := &cobra.Command{
		Use:   "unread",
		Short: "Count unread notifications, messages and invitations",
		Long: "Count what waits for you, as the badges of LinkedIn's top bar show it (one request).\n" +
			"With --folders, also count unread conversations in each message folder (two more requests).",
		Example: "  lmw unread\n  lmw unread --folders\n  lmw unread --watch 10m\n  lmw unread --watch 15m --on-change 'notify-send LinkedIn \"$LMW_NEW_MESSAGES new messages\"'",
		Args:    exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if onChange != "" && every == 0 {
				return errs.New(errs.Usage, "--on-change needs --watch: lmw unread --watch 15m --on-change COMMAND")
			}
			if every != 0 {
				return a.watchUnread(cmd.Context(), every, folders, onChange)
			}
			unread, err := fetch(a, func(c *voyager.Client) (api.Unread, error) {
				return api.UnreadCounts(cmd.Context(), c, folders)
			})
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, unread)
			}
			a.printf("%s\n", output.Unread(unread))
			return nil
		},
	}
	cmd.Flags().BoolVar(&folders, "folders", false, "also count unread conversations by message folder")
	cmd.Flags().DurationVar(&every, "watch", 0, "keep counting every DURATION (10m, 1h) and print a line when something changes; Ctrl-C stops")
	cmd.Flags().StringVar(&onChange, "on-change", "", "with --watch, run COMMAND in your shell when a count goes up (counts in LMW_* variables)")
	return cmd
}

// watchUnread counts unread items every so often until interrupted, and
// prints the counts when they change. It stops at the first error, so a
// pushback from LinkedIn ends the watch instead of insisting.
func (a *app) watchUnread(ctx context.Context, every time.Duration, folders bool, onChange string) error {
	perSweep := 1
	if folders {
		perSweep = 3
	}
	// It may use what watches may (see watchBudgetPerHour).
	budget := a.watchBudgetPerHour()
	if every <= 0 || float64(perSweep)*float64(time.Hour)/float64(every) > budget {
		least := (time.Duration(float64(perSweep)*float64(time.Hour)/budget) + time.Minute - 1).Truncate(time.Minute)
		return errs.New(errs.Usage, "--watch %s would use more than %s requests an hour; watch every %s or more", every, rate(budget), least)
	}
	a.note("Watching every %s (%d requests an hour). Ctrl-C stops.\n", every, perSweep*int(time.Hour/every))
	var last *api.Unread
	for ctx.Err() == nil {
		unread, err := fetch(a, func(c *voyager.Client) (api.Unread, error) { return api.UnreadCounts(ctx, c, folders) })
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if last == nil || !reflect.DeepEqual(*last, unread) {
			if err := a.printSweep(unread, last); err != nil {
				return err
			}
			if onChange != "" && last != nil && wentUp(*last, unread) {
				if err := a.hook(ctx, onChange, hookEnv(*last, unread)); err != nil && ctx.Err() == nil {
					fmt.Fprintf(a.stderr, "lmw: the --on-change command failed: %v\n", err)
				}
			}
			last = &unread
		}
		a.wait(every)
	}
	return nil
}

// wentUp reports whether any count of now is higher than before.
func wentUp(before, now api.Unread) bool {
	if now.Notifications > before.Notifications || now.Messages > before.Messages || now.Network > before.Network {
		return true
	}
	for name, n := range now.Folders {
		if n > before.Folders[name] {
			return true
		}
	}
	return false
}

// hookEnv tells an --on-change command the counts, what went up, and all
// of it as JSON.
func hookEnv(before, now api.Unread) []string {
	counts, _ := json.Marshal(now)
	return []string{
		fmt.Sprintf("LMW_NOTIFICATIONS=%d", now.Notifications),
		fmt.Sprintf("LMW_MESSAGES=%d", now.Messages),
		fmt.Sprintf("LMW_NETWORK=%d", now.Network),
		fmt.Sprintf("LMW_NEW_NOTIFICATIONS=%d", max(0, now.Notifications-before.Notifications)),
		fmt.Sprintf("LMW_NEW_MESSAGES=%d", max(0, now.Messages-before.Messages)),
		fmt.Sprintf("LMW_NEW_NETWORK=%d", max(0, now.Network-before.Network)),
		"LMW_UNREAD=" + string(counts),
	}
}

// hook runs a command of the person's in their shell, with env added to
// lmw's environment. Its output goes to stderr, so stdout keeps only lmw's.
func (a *app) hook(ctx context.Context, command string, env []string) error {
	if a.runHook != nil {
		return a.runHook(ctx, command, env)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	}
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = a.stderr, a.stderr
	return cmd.Run()
}

// printSweep prints one radar sweep: the time, the counts and what went up.
func (a *app) printSweep(now api.Unread, before *api.Unread) error {
	at := time.Now().Format("15:04")
	if a.jsonOut {
		return json.NewEncoder(a.stdout).Encode(map[string]any{"time": time.Now().Format(time.RFC3339), "unread": now})
	}
	count := func(name string, n int, was func(api.Unread) int) string {
		if before != nil && n > was(*before) {
			return fmt.Sprintf("%s %d (+%d)", name, n, n-was(*before))
		}
		return fmt.Sprintf("%s %d", name, n)
	}
	a.printf("%s  %s · %s · %s\n", at,
		count("notifications", now.Notifications, func(u api.Unread) int { return u.Notifications }),
		count("messages", now.Messages, func(u api.Unread) int { return u.Messages }),
		count("my network", now.Network, func(u api.Unread) int { return u.Network }))
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *app) mcpNetworkTools() []mcp.Tool {
	invitationID := mcp.Param{Name: "invitation_id", Type: "string", Required: true, Description: "the invitation id from list_invitations"}
	return []mcp.Tool{
		{
			Name:        "get_unread_counts",
			Title:       "Unread counts",
			Description: "How many unread notifications, messages and network invitations wait for the person, as LinkedIn's top bar badges show them. Cheap (one request): check it before reading notifications or messages.",
			ReadOnly:    true,
			Params:      []mcp.Param{{Name: "folders", Type: "boolean", Description: "also count unread conversations by message folder (inbox, other, requests, archive, spam); two more requests"}},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				return fetch(a, func(c *voyager.Client) (api.Unread, error) { return api.UnreadCounts(ctx, c, args.Bool("folders")) })
			},
		},
		{
			Name:        "get_notifications",
			Title:       "Notifications",
			Description: "The person's latest LinkedIn notifications, with date, link and whether they were read.",
			ReadOnly:    true,
			Params:      []mcp.Param{limitParam("notifications", 10, api.MaxListItems)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				items, err := fetch(a, func(c *voyager.Client) ([]api.Notification, error) {
					return api.Notifications(ctx, c, args.Int("limit"))
				})
				return listOf("notifications", items, err)
			},
		},
		{
			Name:        "list_invitations",
			Title:       "Connection requests",
			Description: "Pending connection requests: received ones (with their message), or the ones the person sent.",
			ReadOnly:    true,
			Params: []mcp.Param{
				{Name: "direction", Type: "string", Description: "received or sent", Enum: []string{"received", "sent"}, Default: "received"},
				limitParam("invitations", 10, api.MaxListItems),
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				list := api.Invitations
				if args.String("direction") == "sent" {
					list = api.SentInvitations
				}
				items, err := fetch(a, func(c *voyager.Client) ([]api.Invitation, error) { return list(ctx, c, args.Int("limit")) })
				return listOf("invitations", items, err)
			},
		},
		{
			Name:        "list_connections",
			Title:       "Connections",
			Description: "The person's 1st-degree connections, most recently added first.",
			ReadOnly:    true,
			Params:      []mcp.Param{limitParam("connections", 20, api.MaxListItems)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				items, err := fetch(a, func(c *voyager.Client) ([]api.Connection, error) { return api.Connections(ctx, c, args.Int("limit")) })
				return listOf("connections", items, err)
			},
		},
		{
			Name:        "answer_invitation",
			Title:       "Accept or ignore a connection request",
			Description: "Accept or ignore a received connection request.",
			Params: []mcp.Param{invitationID,
				{Name: "action", Type: "string", Description: "accept or ignore", Enum: []string{"accept", "ignore"}, Required: true}},
			Confirm: func(args mcp.Args) (string, error) {
				return fmt.Sprintf("%s LinkedIn invitation %s?", map[string]string{"accept": "Accept", "ignore": "Ignore"}[args.String("action")], args.String("invitation_id")), nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				return fetch(a, func(c *voyager.Client) (any, error) {
					inv, err := api.FindInvitation(ctx, c, args.String("invitation_id"))
					if err != nil {
						return nil, err
					}
					if err := api.RespondToInvitation(ctx, c, inv, api.InvitationAction(args.String("action"))); err != nil {
						return nil, err
					}
					return map[string]any{"id": inv.ID, "name": inv.Name, "action": args.String("action")}, nil
				})
			},
		},
		{
			Name:        "withdraw_invitation",
			Title:       "Withdraw a connection request",
			Description: "Withdraw a connection request the person sent.",
			Params:      []mcp.Param{invitationID},
			Confirm: func(args mcp.Args) (string, error) {
				return "Withdraw your LinkedIn invitation " + args.String("invitation_id") + "?", nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				err := a.withClient(func(c *voyager.Client) error { return api.WithdrawInvitation(ctx, c, args.String("invitation_id")) })
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": args.String("invitation_id"), "withdrawn": true}, nil
			},
		},
		{
			Name:        "send_connection_request",
			Title:       "Send a connection request",
			Description: "Invite a member to connect, with an optional personal note. Only for people the person asked to connect with.",
			Params: []mcp.Param{profileParam,
				{Name: "note", Type: "string", Description: fmt.Sprintf("optional personal note, up to %d characters", api.MaxInvitationNote)}},
			Confirm: func(args mcp.Args) (string, error) {
				question := "Send a LinkedIn connection request to " + args.String("profile") + "?"
				if note := args.String("note"); note != "" {
					question += "\n\nNote: " + note
				}
				return question, nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				ref, err := api.ParseProfile(args.String("profile"))
				if err != nil {
					return nil, err
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.Connect(ctx, c, ref, args.String("note")) }); err != nil {
					return nil, err
				}
				return map[string]any{"profile": ref.String(), "sent": true}, nil
			},
		},
		{
			Name:        "remove_connection",
			Title:       "Remove a connection",
			Description: "Remove a member from the person's connections.",
			Destructive: true,
			Params:      []mcp.Param{profileParam},
			Confirm: func(args mcp.Args) (string, error) {
				return "Remove " + args.String("profile") + " from your LinkedIn connections?", nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				ref, err := api.ParseProfile(args.String("profile"))
				if err != nil {
					return nil, err
				}
				if ref.PublicID == "" {
					return nil, errs.New(errs.Usage, "give the profile address or name, not a member id")
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.RemoveConnection(ctx, c, ref) }); err != nil {
					return nil, err
				}
				return map[string]any{"profile": ref.String(), "removed": true}, nil
			},
		},
	}
}
