package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/notify"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/watch"
)

func (a *app) watches() watch.Store   { return watch.Store{Path: a.cfg.WatchesFile()} }
func (a *app) channels() notify.Store { return notify.Store{Path: a.cfg.NotifyFile()} }

func (a *app) sender() notify.Sender {
	return notify.Sender{Shell: a.runHook, Output: a.stderr}
}

// watchBudgetPerHour is how many requests an hour all watches together may
// use: a third of the hourly budget, and no more than half the daily budget
// over a whole day, so lmw keeps working by hand.
func (a *app) watchBudgetPerHour() float64 {
	return math.Floor(min(float64(a.cfg.HourlyRequestBudget)/3, float64(a.cfg.DailyRequestBudget)/2/24))
}

// rate writes requests an hour with at most one decimal.
func rate(perHour float64) string {
	return strconv.FormatFloat(math.Round(perHour*10)/10, 'f', -1, 64)
}

// newWatch is what a person or an assistant asks to watch.
type newWatch struct {
	kind, target, channel, note string
	every                       time.Duration
	once                        bool
	location, workplace, posted string
}

// addWatch checks the watch, looks at LinkedIn once so that only what comes
// next is new, and stores it.
func (a *app) addWatch(ctx context.Context, n newWatch) (watch.Watch, error) {
	if n.every%time.Minute != 0 {
		return watch.Watch{}, errs.New(errs.Usage, "give --every in whole minutes, such as 30m or 2h")
	}
	w, err := watch.New(n.kind, strings.TrimSpace(n.target), int(n.every/time.Minute))
	if err != nil {
		return watch.Watch{}, errs.New(errs.Usage, "%s", err)
	}
	w.Once, w.Note, w.Channel = n.once, n.note, n.channel
	for key, value := range map[string]string{"location": n.location, "workplace": n.workplace, "posted": n.posted} {
		if value != "" {
			if w.Kind != "jobs" {
				return watch.Watch{}, errs.New(errs.Usage, "--%s is only for jobs watches", key)
			}
			w.Params[key] = value
		}
	}
	if n.channel != "" {
		channels, err := a.channels().Load()
		if err != nil {
			return watch.Watch{}, errs.Wrap(errs.Storage, err, "could not read the notification channels: %v", err)
		}
		if _, err := notify.Pick(channels, n.channel); err != nil {
			return watch.Watch{}, errs.New(errs.Usage, "%s", err)
		}
	}
	existing, err := a.watches().Load()
	if err != nil {
		return watch.Watch{}, errs.Wrap(errs.Storage, err, "could not read the watches: %v", err)
	}
	if total := watch.PerHour(append(existing, w)); total > a.watchBudgetPerHour() {
		return watch.Watch{}, errs.New(errs.Usage, "with this one, watches would use %s requests an hour, more than the %s they may (see lmw watch --help); check less often or remove a watch",
			rate(total), rate(a.watchBudgetPerHour()))
	}
	err = a.withClient(func(c *voyager.Client) error {
		if err := watch.Prepare(ctx, c, &w); err != nil {
			return err
		}
		_, err := watch.Check(ctx, c, &w, time.Now())
		return err
	})
	if err != nil {
		return watch.Watch{}, err
	}
	return w, a.watches().Update(func(ws []watch.Watch) ([]watch.Watch, error) { return append(ws, w), nil })
}

// removeWatch deletes the watch with id.
func (a *app) removeWatch(id string) (watch.Watch, error) {
	var removed watch.Watch
	err := a.watches().Update(func(ws []watch.Watch) ([]watch.Watch, error) {
		for i, w := range ws {
			if w.ID == id {
				removed = w
				return append(ws[:i], ws[i+1:]...), nil
			}
		}
		return nil, errs.New(errs.Usage, "there is no watch %q; see lmw watch list", id)
	})
	return removed, err
}

// sweepResult is what one check of a watch found.
type sweepResult struct {
	Watch string           `json:"watch"`
	About string           `json:"about"`
	Found []notify.Message `json:"found"`
	Error string           `json:"error,omitempty"`
	Ended bool             `json:"ended,omitempty"`
}

// sweep checks the watches that are due (all of them with all), sends what
// they found to the channels and stores what they saw. A budget, cooldown or
// security check from LinkedIn ends the sweep: the other watches would only
// meet it too.
func (a *app) sweep(ctx context.Context, all, send bool) ([]sweepResult, error) {
	watches, err := a.watches().Load()
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "could not read the watches: %v", err)
	}
	channels, err := a.channels().Load()
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "could not read the notification channels: %v", err)
	}
	var results []sweepResult
	now := time.Now()
	for _, w := range watches {
		if !all && !w.Due(now) {
			continue
		}
		failing := w.LastError != ""
		var found []notify.Message
		checkErr := a.withClient(func(c *voyager.Client) (err error) {
			found, err = watch.Check(ctx, c, &w, time.Now())
			return err
		})
		if ctx.Err() != nil {
			return results, nil
		}
		result := sweepResult{Watch: w.ID, About: w.Describe(), Found: found}
		if found == nil {
			result.Found = []notify.Message{}
		}
		if checkErr != nil {
			result.Error = checkErr.Error()
			if stopsSweep(checkErr) {
				// Not the watch's fault: leave it as it was, to try again.
				results = append(results, result)
				return results, checkErr
			}
			if !failing && send {
				a.deliver(ctx, channels, w.Channel, notify.Message{Title: "lmw: a watch stopped working", Text: w.Describe() + ": " + checkErr.Error(), Watch: w.ID})
			}
		}
		if send {
			for _, m := range found {
				a.deliver(ctx, channels, w.Channel, m)
			}
		}
		result.Ended = w.Once && len(found) > 0
		if err := a.watches().Update(func(ws []watch.Watch) ([]watch.Watch, error) {
			for i := range ws {
				if ws[i].ID == w.ID {
					if result.Ended {
						return append(ws[:i], ws[i+1:]...), nil
					}
					ws[i] = w
				}
			}
			return ws, nil
		}); err != nil {
			return results, errs.Wrap(errs.Storage, err, "could not save the watches: %v", err)
		}
		results = append(results, result)
	}
	return results, nil
}

// stopsSweep reports whether err means LinkedIn wants lmw to stop for now.
func stopsSweep(err error) bool {
	var e *errs.Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Kind {
	case errs.Budget, errs.Cooldown, errs.RateLimited, errs.Challenge, errs.AuthRequired:
		return true
	}
	return false
}

// deliver sends m to the watch's channel (every channel when it has none).
// Failures go to stderr and are not fatal; stdout stays free, since the MCP
// server speaks on it.
func (a *app) deliver(ctx context.Context, channels []notify.Channel, name string, m notify.Message) {
	picked, err := notify.Pick(channels, name)
	if err != nil {
		fmt.Fprintf(a.stderr, "lmw: %v; sending to every channel\n", err)
		picked = channels
	}
	for _, c := range picked {
		if err := a.sender().Send(ctx, c.Target, m); err != nil {
			fmt.Fprintf(a.stderr, "lmw: could not notify %s: %v\n", c.Name, err)
		}
	}
}

// printFound prints one line for each thing the watches found.
func (a *app) printFound(results []sweepResult) {
	for _, r := range results {
		for _, m := range r.Found {
			line := time.Now().Format("2006-01-02 15:04") + "  " + m.Title
			if m.URL != "" {
				line += "  " + m.URL
			}
			a.printf("%s\n", line)
		}
		if r.Error != "" {
			a.printf("%s  %s %s: %s\n", time.Now().Format("2006-01-02 15:04"), r.Watch, r.About, r.Error)
		}
	}
}

func (a *app) watchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Keep an eye on LinkedIn for you: new posts of someone, jobs, messages...",
		Long: `Watches tell you when something happens on LinkedIn: the next post of
someone, new jobs for a search, new messages or connection requests. Add
them here (or let an AI assistant add them through MCP), and run
` + "`lmw daemon`" + ` on a computer that stays on to check them and notify
you through the channels of ` + "`lmw notify`" + `.

Kinds of watches:
` + watchKindsHelp() + `
All watches together may use a third of the hourly request budget, and half
the daily budget over a day: about 6 requests an hour by default. A posts
watch every 30 minutes uses 2.`,
	}
	var n newWatch
	add := &cobra.Command{
		Use:   "add KIND [TARGET]",
		Short: "Add a watch",
		Example: `  lmw watch add posts linkedin.com/in/grace-hopper --once
  lmw watch add jobs "go developer" --location Spain --workplace remote --every 2h
  lmw watch add messages --to phone
  lmw watch add comments https://www.linkedin.com/feed/update/urn:li:activity:7123456789012345678/`,
		Args: rangeArgs(1, 2, "the kind of watch and, for some kinds, what to watch"),
		RunE: func(cmd *cobra.Command, args []string) error {
			n.kind, n.target = args[0], ""
			if len(args) == 2 {
				n.target = args[1]
			}
			w, err := a.addWatch(cmd.Context(), n)
			if err != nil {
				return err
			}
			channels, _ := a.channels().Load()
			if len(channels) == 0 {
				a.note("There is no notification channel yet: add one with `lmw notify add`.\n")
			}
			return a.done(fmt.Sprintf("Watching %s every %s (id %s). `lmw daemon` checks it.", w.Describe(), w.Every(), w.ID), map[string]any{"watch": w})
		},
	}
	add.Flags().DurationVar(&n.every, "every", 30*time.Minute, "how often to check (15m or more)")
	add.Flags().BoolVar(&n.once, "once", false, "end the watch after its first notification (the next post, the next message)")
	add.Flags().StringVar(&n.channel, "to", "", "the notification channel to use (default: all of them)")
	add.Flags().StringVar(&n.note, "note", "", "why you watch this, shown in the list")
	add.Flags().StringVar(&n.location, "location", "", "jobs: where the job is")
	add.Flags().StringVar(&n.workplace, "workplace", "", "jobs: onsite, remote or hybrid, comma separated")
	add.Flags().StringVar(&n.posted, "posted", "", "jobs: posted in the last day, week or month")

	list := &cobra.Command{
		Use:   "list",
		Short: "List your watches",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(*cobra.Command, []string) error {
			watches, err := a.watches().Load()
			if err != nil {
				return errs.Wrap(errs.Storage, err, "could not read the watches: %v", err)
			}
			if a.jsonOut {
				if watches == nil {
					watches = []watch.Watch{}
				}
				return output.JSON(a.stdout, watches)
			}
			if len(watches) == 0 {
				a.printf("No watches. Add one with `lmw watch add`.\n")
				return nil
			}
			for _, w := range watches {
				a.printf("%s\n", output.Watch(w))
			}
			a.printf("\n%s requests an hour of %s allowed for watches.\n", rate(watch.PerHour(watches)), rate(a.watchBudgetPerHour()))
			return nil
		},
	}
	remove := &cobra.Command{
		Use:   "remove ID",
		Short: "Remove a watch",
		Args:  exactArgs(1, "the id of the watch (see lmw watch list)"),
		RunE: func(_ *cobra.Command, args []string) error {
			w, err := a.removeWatch(args[0])
			if err != nil {
				return err
			}
			return a.done("Stopped watching "+w.Describe()+".", map[string]any{"removed": w.ID})
		},
	}
	var all, quiet bool
	run := &cobra.Command{
		Use:   "run",
		Short: "Check the watches that are due once, and notify (for cron)",
		Long: "Check the watches that are due, send what they found to your channels, and exit.\n" +
			"Run it from cron or a scheduled task instead of `lmw daemon`.",
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			results, err := a.sweep(cmd.Context(), all, !quiet)
			if a.jsonOut {
				if results == nil {
					results = []sweepResult{}
				}
				if jsonErr := output.JSON(a.stdout, results); jsonErr != nil {
					return jsonErr
				}
			} else if a.printFound(results); len(results) == 0 && err == nil {
				a.note("No watch is due.\n")
			}
			return err
		},
	}
	run.Flags().BoolVar(&all, "all", false, "check every watch, due or not")
	run.Flags().BoolVar(&quiet, "no-notify", false, "only print what is new; send nothing")
	cmd.AddCommand(add, list, remove, run)
	return cmd
}

func watchKindsHelp() string {
	var lines []string
	for _, k := range watch.Kinds {
		line := fmt.Sprintf("  %-14s %s", k.Name, k.About)
		if k.Target != "" {
			line += " (TARGET: " + k.Target + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *app) daemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Keep checking your watches and send notifications (Ctrl-C stops)",
		Long: `Check your watches as they fall due and send what they find to your
notification channels, until stopped. Run it where it can stay on, such as a
small server; the guide "lmw on a server" shows how to keep it running.

When LinkedIn pushes back (a cooldown, a security check, an expired session),
the daemon waits instead of insisting, and tells you once.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.daemon(cmd.Context())
		},
	}
}

// daemonPause is how long the daemon waits after LinkedIn pushed back.
const daemonPause = time.Hour

func (a *app) daemon(ctx context.Context) error {
	watches, err := a.watches().Load()
	if err != nil {
		return errs.Wrap(errs.Storage, err, "could not read the watches: %v", err)
	}
	a.note("lmw daemon: %d watches, %s requests an hour. Ctrl-C stops.\n", len(watches), rate(watch.PerHour(watches)))
	var told string
	for ctx.Err() == nil {
		results, err := a.sweep(ctx, false, true)
		a.printFound(results)
		if ctx.Err() != nil {
			return nil
		}
		pause := a.nextSweep()
		if err != nil {
			fmt.Fprintf(a.stderr, "lmw: %v; waiting %s\n", err, daemonPause)
			if err.Error() != told {
				channels, _ := a.channels().Load()
				a.deliver(ctx, channels, "", notify.Message{Title: "lmw daemon is waiting", Text: err.Error()})
				told = err.Error()
			}
			pause = daemonPause
		} else {
			told = ""
		}
		a.wait(pause)
	}
	return nil
}

// nextSweep is how long until the next watch falls due, between one and
// five minutes so new watches are picked up soon.
func (a *app) nextSweep() time.Duration {
	watches, _ := a.watches().Load()
	next := 5 * time.Minute
	now := time.Now()
	for _, w := range watches {
		if until := w.LastRun.Add(w.Every()).Sub(now); until < next {
			next = until
		}
	}
	return max(next, time.Minute)
}

func (a *app) notifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Choose where lmw sends notifications: phone, Telegram, Discord...",
		Long: `Notification channels are where watches (and your AI assistant) tell you
about things. A channel has a name and a target:

` + notifyKindsHelp() + `
Targets can hold tokens: lmw keeps them in a file only you can read, and
lists them masked.`,
	}
	add := &cobra.Command{
		Use:     "add NAME TARGET",
		Short:   "Add a notification channel",
		Example: "  lmw notify add phone ntfy:my-secret-topic\n  lmw notify add telegram telegram:123456:ABC-token:987654",
		Args:    exactArgs(2, "a name and a target"),
		RunE: func(_ *cobra.Command, args []string) error {
			name, target := args[0], args[1]
			if err := notify.Validate(target); err != nil {
				return errs.New(errs.Usage, "%s", err)
			}
			channels, err := a.channels().Load()
			if err != nil {
				return errs.Wrap(errs.Storage, err, "could not read the notification channels: %v", err)
			}
			for _, c := range channels {
				if c.Name == name {
					return errs.New(errs.Usage, "there is a channel called %q already; remove it first", name)
				}
			}
			if err := a.channels().Save(append(channels, notify.Channel{Name: name, Target: target})); err != nil {
				return errs.Wrap(errs.Storage, err, "could not save the channel: %v", err)
			}
			return a.done("Added "+name+" ("+notify.Mask(target)+"). Try it with `lmw notify test "+name+"`.", map[string]any{"name": name, "target": notify.Mask(target)})
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List the notification channels, tokens masked",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(*cobra.Command, []string) error {
			channels, err := a.maskedChannels()
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, channels)
			}
			if len(channels) == 0 {
				a.printf("No notification channels. Add one with `lmw notify add`.\n")
			}
			for _, c := range channels {
				a.printf("%-12s %s\n", c.Name, c.Target)
			}
			return nil
		},
	}
	remove := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a notification channel",
		Args:  exactArgs(1, "the name of the channel"),
		RunE: func(_ *cobra.Command, args []string) error {
			channels, err := a.channels().Load()
			if err != nil {
				return errs.Wrap(errs.Storage, err, "could not read the notification channels: %v", err)
			}
			for i, c := range channels {
				if c.Name == args[0] {
					if err := a.channels().Save(append(channels[:i], channels[i+1:]...)); err != nil {
						return errs.Wrap(errs.Storage, err, "could not save the channels: %v", err)
					}
					return a.done("Removed "+c.Name+".", map[string]any{"removed": c.Name})
				}
			}
			return errs.New(errs.Usage, "there is no channel %q; see lmw notify list", args[0])
		},
	}
	var to, title, link string
	send := &cobra.Command{
		Use:     "send TEXT",
		Short:   "Send a notification of yours",
		Example: "  lmw notify send \"Backup finished\" --to phone",
		Args:    exactArgs(1, "the text to send"),
		RunE: func(cmd *cobra.Command, args []string) error {
			sent, err := a.sendNotification(cmd.Context(), to, notify.Message{Title: firstNonEmptyString(title, "lmw"), Text: args[0], URL: link})
			if err != nil {
				return err
			}
			return a.done("Sent to "+strings.Join(sent, ", ")+".", map[string]any{"sent": sent})
		},
	}
	send.Flags().StringVar(&to, "to", "", "the channel (default: all of them)")
	send.Flags().StringVar(&title, "title", "", "the title (default: lmw)")
	send.Flags().StringVar(&link, "url", "", "a link to open")
	test := &cobra.Command{
		Use:   "test [NAME]",
		Short: "Send a test notification",
		Args:  rangeArgs(0, 1, "the channel to test (default: all of them)"),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			sent, err := a.sendNotification(cmd.Context(), name, notify.Message{Title: "lmw is on the radio", Text: "Notifications from lmw reach you here."})
			if err != nil {
				return err
			}
			return a.done("Sent to "+strings.Join(sent, ", ")+".", map[string]any{"sent": sent})
		},
	}
	cmd.AddCommand(add, list, remove, send, test)
	return cmd
}

func notifyKindsHelp() string {
	var lines []string
	for _, k := range notify.Kinds {
		lines = append(lines, fmt.Sprintf("  %-9s %s\n            %s", k.Scheme, k.About, k.Example))
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *app) maskedChannels() ([]notify.Channel, error) {
	channels, err := a.channels().Load()
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "could not read the notification channels: %v", err)
	}
	masked := []notify.Channel{}
	for _, c := range channels {
		masked = append(masked, notify.Channel{Name: c.Name, Target: notify.Mask(c.Target)})
	}
	return masked, nil
}

// sendNotification sends m to the channel named (all with ""), and fails
// if it reached none.
func (a *app) sendNotification(ctx context.Context, name string, m notify.Message) ([]string, error) {
	channels, err := a.channels().Load()
	if err != nil {
		return nil, errs.Wrap(errs.Storage, err, "could not read the notification channels: %v", err)
	}
	if len(channels) == 0 {
		return nil, errs.New(errs.Usage, "there are no notification channels; add one with `lmw notify add`")
	}
	picked, err := notify.Pick(channels, name)
	if err != nil {
		return nil, errs.New(errs.Usage, "%s", err)
	}
	var sent, failed []string
	for _, c := range picked {
		if err := a.sender().Send(ctx, c.Target, m); err != nil {
			failed = append(failed, c.Name+": "+err.Error())
			continue
		}
		sent = append(sent, c.Name)
	}
	if len(sent) == 0 {
		return nil, errs.New(errs.API, "the notification reached no channel: %s", strings.Join(failed, "; "))
	}
	for _, f := range failed {
		fmt.Fprintf(a.stderr, "lmw: could not notify %s\n", f)
	}
	return sent, nil
}

func (a *app) mcpWatchTools() []mcp.Tool {
	kinds := make([]string, len(watch.Kinds))
	var about []string
	for i, k := range watch.Kinds {
		kinds[i] = k.Name
		line := k.Name + ": " + k.About
		if k.Target != "" {
			line += " (target: " + k.Target + ")"
		}
		about = append(about, line)
	}
	return []mcp.Tool{
		{
			Name:  "create_watch",
			Title: "Watch LinkedIn for the person",
			Description: "Keep an eye on something and notify the person when it happens, e.g. \"tell me when Grace Hopper posts next\" (posts, once) or \"new remote Go jobs\" (jobs). " +
				"Kinds: " + strings.Join(about, "; ") + ". The first check happens now, so only what comes after is new. `lmw daemon` must run for later checks.",
			Local: true,
			Params: []mcp.Param{
				{Name: "kind", Type: "string", Enum: kinds, Required: true, Description: "what to watch"},
				{Name: "target", Type: "string", Description: "the profile, post or job keywords, for the kinds that take one"},
				{Name: "every_minutes", Type: "integer", Default: 30, Min: watch.MinMinutes, Max: 24 * 60, Description: "how often to check"},
				{Name: "once", Type: "boolean", Description: "end the watch after the first notification (\"the next post\")"},
				{Name: "channel", Type: "string", Description: "the notification channel (default: all of them)"},
				{Name: "note", Type: "string", Description: "why the person asked for it, in their words"},
				{Name: "location", Type: "string", Description: "jobs: where the job is"},
				{Name: "workplace", Type: "string", Description: "jobs: onsite, remote or hybrid, comma separated"},
				{Name: "posted", Type: "string", Enum: []string{"day", "week", "month"}, Description: "jobs: posted in the last day, week or month"},
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				w, err := a.addWatch(ctx, newWatch{kind: args.String("kind"), target: args.String("target"), every: time.Duration(args.Int("every_minutes")) * time.Minute,
					once: args.Bool("once"), channel: args.String("channel"), note: args.String("note"),
					location: args.String("location"), workplace: args.String("workplace"), posted: args.String("posted")})
				if err != nil {
					return nil, err
				}
				return map[string]any{"watch": w.ID, "watching": w.Describe(), "every_minutes": w.Minutes, "once": w.Once}, nil
			},
		},
		{
			Name:        "list_watches",
			Title:       "Watches",
			Description: "The watches the person has: what each one watches, how often, when it last ran and any error.",
			ReadOnly:    true,
			Run: func(context.Context, mcp.Args) (any, error) {
				watches, err := a.watches().Load()
				if err != nil {
					return nil, err
				}
				list := []map[string]any{}
				for _, w := range watches {
					list = append(list, map[string]any{"id": w.ID, "watching": w.Describe(), "every_minutes": w.Minutes, "once": w.Once, "channel": w.Channel,
						"note": w.Note, "last_run": w.LastRun, "last_error": w.LastError, "found": w.Found})
				}
				return map[string]any{"watches": list, "requests_per_hour": watch.PerHour(watches), "allowed_per_hour": a.watchBudgetPerHour()}, nil
			},
		},
		{
			Name:        "delete_watch",
			Title:       "Stop a watch",
			Description: "Remove one of the person's watches.",
			Local:       true,
			Params:      []mcp.Param{{Name: "id", Type: "string", Required: true, Description: "the watch id from list_watches"}},
			Run: func(_ context.Context, args mcp.Args) (any, error) {
				w, err := a.removeWatch(args.String("id"))
				if err != nil {
					return nil, err
				}
				return map[string]any{"removed": w.ID, "was_watching": w.Describe()}, nil
			},
		},
		{
			Name:        "check_watches",
			Title:       "Check watches now",
			Description: "Check the watches that are due (or all) now and return what is new. Use it when no daemon runs. It sends what it finds to the person's channels unless notify is false.",
			Local:       true,
			Params: []mcp.Param{
				{Name: "all", Type: "boolean", Description: "check every watch, due or not"},
				{Name: "notify", Type: "boolean", Default: true, Description: "send what is new to the channels"},
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				results, err := a.sweep(ctx, args.Bool("all"), args.Bool("notify"))
				if results == nil {
					results = []sweepResult{}
				}
				if err != nil {
					return map[string]any{"checked": results, "stopped": err.Error()}, nil
				}
				return map[string]any{"checked": results}, nil
			},
		},
		{
			Name:        "list_notification_channels",
			Title:       "Notification channels",
			Description: "Where notifications go (names, tokens masked).",
			ReadOnly:    true,
			Run: func(context.Context, mcp.Args) (any, error) {
				channels, err := a.maskedChannels()
				return map[string]any{"channels": channels}, err
			},
		},
		{
			Name:        "send_notification",
			Title:       "Notify the person",
			Description: "Send the person a short notification through their channels (phone, Telegram...), e.g. a summary they asked to receive.",
			Local:       true,
			Params: []mcp.Param{
				{Name: "text", Type: "string", Required: true, Description: "the message"},
				{Name: "title", Type: "string", Description: "a short title"},
				{Name: "url", Type: "string", Description: "a link to open"},
				{Name: "channel", Type: "string", Description: "the channel (default: all of them)"},
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				sent, err := a.sendNotification(ctx, args.String("channel"), notify.Message{Title: firstNonEmptyString(args.String("title"), "lmw"), Text: args.String("text"), URL: args.String("url")})
				return map[string]any{"sent": sent}, err
			},
		},
	}
}
