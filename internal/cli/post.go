package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/postgen"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/voyager"
)

func (a *app) postCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "post",
		Short: "Write, publish, read and react to posts",
	}
	cmd.AddCommand(a.postDraftCommand(), a.postPreviewCommand(), a.postPublishCommand(),
		a.postShowCommand(), a.postCommentsCommand(), a.postReactionsCommand(),
		a.postReactCommand(), a.postUnreactCommand(), a.postCommentCommand(), a.postReplyCommand(),
		a.postEditCommand(), a.postDeleteCommand())
	return cmd
}

func (a *app) postDraftCommand() *cobra.Command {
	var tone, length, outputFile string
	cmd := &cobra.Command{
		Use:   "draft IDEA...",
		Short: "Build a post outline from an idea (offline)",
		Long: "Build a post outline from an idea, without contacting LinkedIn.\n\n" +
			"The outline has a hook, sections to fill in and a closing question.\n" +
			"Replace the [placeholders] before publishing.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errs.New(errs.Usage, "expected an idea, e.g. lmw post draft \"What I learned shipping a CLI\"")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			draft, err := postgen.Generate(strings.Join(args, " "), tone, length)
			if err != nil {
				return err
			}
			if outputFile != "" {
				if err := os.WriteFile(outputFile, []byte(draft.Text+"\n"), 0o644); err != nil {
					return errs.Wrap(errs.Storage, err, "cannot write %s: %v", outputFile, err)
				}
			}
			if a.jsonOut {
				return output.JSON(a.stdout, draft)
			}
			a.printf("%s\nReplace the [placeholders] before publishing.\n", output.PostPreview(draft.Text))
			if outputFile != "" {
				a.printf("Saved to %s.\n", outputFile)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tone, "tone", "professional", "tone: "+strings.Join(postgen.Tones, ", "))
	cmd.Flags().StringVar(&length, "length", "medium", "length: "+strings.Join(postgen.Lengths, ", "))
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "also save the outline to this file")
	return cmd
}

// textSource is a text given with --text or --text-file; what names it in
// help and errors ("post" when empty).
type textSource struct{ what, text, file string }

func (s *textSource) name() string { return firstNonEmptyString(s.what, "post") }

func (s *textSource) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.text, "text", "", s.name()+" text")
	cmd.Flags().StringVar(&s.file, "text-file", "", "file with the "+s.name()+" text")
}

func (s *textSource) read() (string, error) {
	switch {
	case s.text != "" && s.file != "":
		return "", errs.New(errs.Usage, "use either --text or --text-file, not both")
	case s.text == "" && s.file == "":
		return "", errs.New(errs.Usage, "give the %s with --text or --text-file", s.name())
	case s.file == "":
		return postgen.Normalize(s.text), nil
	}
	raw, err := os.ReadFile(s.file)
	if err != nil {
		return "", errs.Wrap(errs.Usage, err, "cannot read %s: %v", s.file, err)
	}
	return postgen.Normalize(string(raw)), nil
}

func (a *app) postPreviewCommand() *cobra.Command {
	var source textSource
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview a post and check that it can be published (offline)",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			text, err := source.read()
			if err != nil {
				return err
			}
			a.printf("%s\n", output.PostPreview(text))
			for _, p := range postgen.Placeholders(text) {
				a.printf("Unfilled placeholder: %s\n", p)
			}
			if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
				return err
			}
			a.printf("Ready to publish.\n")
			return nil
		},
	}
	source.register(cmd)
	return cmd
}

// postExtras are the flags that add something to a post.
type postExtras struct {
	images, alts           []string
	video, document, title string
	poll, duration, at     string
	options                []string
	linkPreview            bool
}

func (e *postExtras) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringArrayVar(&e.images, "image", nil, fmt.Sprintf("attach an image: png, jpg, gif or webp (repeatable, up to %d)", api.MaxImages))
	f.StringArrayVar(&e.alts, "alt", nil, "describe the images for screen readers, in the same order (repeatable)")
	f.StringVar(&e.video, "video", "", "attach a video: mp4 or mov")
	f.StringVar(&e.document, "document", "", "attach a PDF, shown as a swipeable document")
	f.StringVar(&e.title, "title", "", "the title of the document (required with --document)")
	f.StringVar(&e.poll, "poll", "", "add a poll with this question")
	f.StringArrayVar(&e.options, "option", nil, fmt.Sprintf("a poll option (repeatable, 2 to %d)", api.MaxPollOptions))
	f.StringVar(&e.duration, "duration", "1w", "how long the poll runs: "+strings.Join(api.PollDurations, ", "))
	f.BoolVar(&e.linkPreview, "link-preview", false, "show the card of the first link in the text")
	f.StringVar(&e.at, "at", "", `schedule the post for this local time, e.g. "2026-11-05 09:30"`)
}

// build builds and checks what the flags add to a post.
func (e *postExtras) build(now time.Time) (api.PostOptions, error) {
	var opts api.PostOptions
	if len(e.alts) > len(e.images) {
		return opts, errUsage("there are more --alt texts than --image files")
	}
	for i, path := range e.images {
		image, err := api.NewAttachment(path, api.Image)
		if err != nil {
			return opts, err
		}
		if i < len(e.alts) {
			image.Alt = e.alts[i]
		}
		opts.Attachments = append(opts.Attachments, image)
	}
	if e.video != "" {
		video, err := api.NewAttachment(e.video, api.Video)
		if err != nil {
			return opts, err
		}
		opts.Attachments = append(opts.Attachments, video)
	}
	switch {
	case e.document != "":
		document, err := api.NewAttachment(e.document, api.Document)
		if err != nil {
			return opts, err
		}
		document.Title = e.title
		opts.Attachments = append(opts.Attachments, document)
	case e.title != "":
		return opts, errUsage("--title is the title of a --document")
	}
	switch {
	case e.poll != "":
		opts.Poll = &api.Poll{Question: e.poll, Options: e.options, Duration: e.duration}
	case len(e.options) > 0:
		return opts, errUsage("--option needs --poll")
	}
	opts.LinkPreview = e.linkPreview
	if e.at != "" {
		at, err := api.ParseSchedule(e.at, now)
		if err != nil {
			return opts, err
		}
		opts.At = at
	}
	return opts, opts.Check(now)
}

// describeExtras lists what a post carries besides its text, for its confirmation.
func describeExtras(text string, opts api.PostOptions) string {
	var lines []string
	for _, attachment := range opts.Attachments {
		lines = append(lines, "Attached: "+attachment.Describe())
	}
	if p := opts.Poll; p != nil {
		lines = append(lines, fmt.Sprintf("Poll, open for %s: %s", p.Duration, p.Question))
		for _, option := range p.Options {
			lines = append(lines, "  ○ "+option)
		}
	}
	if opts.LinkPreview {
		lines = append(lines, "With the card of "+firstNonEmptyString(api.FirstLink(text), "(no link found)"))
	}
	if !opts.At.IsZero() {
		lines = append(lines, "Scheduled for: "+opts.At.Format("Mon 2 Jan 2006 15:04 MST"))
	}
	return strings.Join(lines, "\n")
}

func (a *app) postPublishCommand() *cobra.Command {
	var source textSource
	var extras postExtras
	var connectionsOnly bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a post, with images, a video, a document or a poll (asks for confirmation)",
		Long: "Publish a post after showing it and asking you to type 'publish'.\n\n" +
			"A post can carry images, a video, a PDF document, a poll or the card of the\n" +
			"first link in its text (one of these), and can be scheduled with --at:\n" +
			"LinkedIn then publishes it at that time. Files are uploaded after you\n" +
			"confirm; nothing is posted if an upload fails.\n\n" +
			"If LinkedIn rejects the request, its API for creating posts may have changed:\n" +
			"publish one post from the website while recording a HAR, then check it with\n" +
			"`lmw har inspect FILE --filter contentcreation`.",
		Example: "  lmw post publish --text-file post.txt\n" +
			"  lmw post publish --text \"Our new engine\" --image engine.png --alt \"The engine, front view\"\n" +
			"  lmw post publish --text \"Slides from my talk\" --document talk.pdf --title \"Engines 101\"\n" +
			"  lmw post publish --text \"Settle this\" --poll \"Tabs or spaces?\" --option Tabs --option Spaces --duration 3d\n" +
			"  lmw post publish --text-file post.txt --at \"2026-11-05 09:30\"",
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			text, err := source.read()
			if err != nil {
				return err
			}
			if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
				return err
			}
			opts, err := extras.build(time.Now())
			if err != nil {
				return err
			}
			audience, visibility := "anyone", api.Anyone
			if connectionsOnly {
				audience, visibility = "your connections only", api.ConnectionsOnly
			}
			a.note("%s\nVisible to: %s\n", output.PostPreview(text), audience)
			if more := describeExtras(text, opts); more != "" {
				a.note("%s\n", more)
			}
			ok, err := a.confirm("Type 'publish' to publish this post on LinkedIn: ", "publish")
			if err != nil || !ok {
				a.note("Cancelled; nothing was published.\n")
				return err
			}

			post, err := fetch(a, func(c *voyager.Client) (api.PublishedPost, error) {
				return api.Publish(cmd.Context(), c, text, visibility, opts)
			})
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, post)
			}
			switch {
			case !opts.At.IsZero():
				a.printf("Scheduled for %s. LinkedIn will publish it then.\n", opts.At.Format("Mon 2 Jan 2006 15:04 MST"))
			case post.URL != "":
				a.printf("Published: %s\n", post.URL)
			default:
				a.printf("Published. LinkedIn did not return the post's address; check your profile.\n")
			}
			return nil
		},
	}
	source.register(cmd)
	extras.register(cmd)
	cmd.Flags().BoolVar(&connectionsOnly, "connections-only", false, "only your connections can see the post")
	return cmd
}

func (a *app) postEditCommand() *cobra.Command {
	var source textSource
	cmd := &cobra.Command{
		Use:   "edit POST",
		Short: "Replace the text of one of your posts (asks for confirmation)",
		Long: "Replace the text of one of your posts. The new text replaces all of the old\n" +
			"one, and mentions in the old text are not kept; images and other media stay.",
		Example: "  lmw post edit urn:li:activity:7123456789012345678 --text-file fixed.txt",
		Args:    exactArgs(1, postArgHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			urn, err := api.ParsePost(args[0])
			if err != nil {
				return err
			}
			text, err := source.read()
			if err != nil {
				return err
			}
			if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
				return err
			}
			if ok, err := a.approve(fmt.Sprintf("Replace the text of %s with:\n%s", urn, output.PostPreview(text)), "edit"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.EditPost(cmd.Context(), c, urn, text) }); err != nil {
				return err
			}
			return a.done("Post edited.", map[string]any{"post": urn, "edited": true})
		},
	}
	source.register(cmd)
	return cmd
}

func (a *app) postReplyCommand() *cobra.Command {
	source := textSource{what: "reply"}
	cmd := &cobra.Command{
		Use:   "reply COMMENT",
		Short: "Reply to a comment (asks for confirmation)",
		Long: "Reply to a comment on a post. COMMENT is the comment's URN, as\n" +
			"`lmw post comments` shows it (urn:li:fsd_comment:(...)).\n\n" +
			"Like comments, replies are never retried: if LinkedIn reports an error, look\n" +
			"at the post before trying again.",
		Example: `  lmw post reply "urn:li:fsd_comment:(7001,urn:li:activity:7123456789012345678)" --text "Thanks!"`,
		Args:    exactArgs(1, "one comment URN, as shown by `lmw post comments`"),
		RunE: func(cmd *cobra.Command, args []string) error {
			thread, err := api.ParseThread(args[0])
			if err != nil {
				return err
			}
			if !api.IsComment(thread) {
				return errUsage("%s is a post; use `lmw post comment` to comment on it", thread)
			}
			text, err := source.read()
			if err != nil {
				return err
			}
			if ok, err := a.approve(fmt.Sprintf("Reply to %s:\n%s", thread, text), "reply"); err != nil || !ok {
				return err
			}
			reply, err := fetch(a, func(c *voyager.Client) (string, error) { return api.CommentOn(cmd.Context(), c, thread, text) })
			if err != nil {
				return err
			}
			return a.done("Reply posted.", map[string]any{"comment": thread, "reply": reply})
		},
	}
	source.register(cmd)
	return cmd
}

func (a *app) mcpPostTools() []mcp.Tool {
	// checkedPost normalizes and validates what the assistant wants to publish.
	checkedPost := func(args mcp.Args) (string, api.Visibility, string, api.PostOptions, error) {
		text := postgen.Normalize(args.String("text"))
		if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
			return "", 0, "", api.PostOptions{}, err
		}
		extras := postExtras{
			images: splitList(args.String("images"), ","), alts: splitList(args.String("alt_texts"), "|"),
			video: args.String("video"), document: args.String("document"), title: args.String("document_title"),
			poll: args.String("poll_question"), options: splitList(args.String("poll_options"), "|"),
			duration: args.String("poll_duration"), linkPreview: args.Bool("link_preview"), at: args.String("schedule_at"),
		}
		opts, err := extras.build(time.Now())
		if err != nil {
			return "", 0, "", opts, err
		}
		if args.String("visibility") == "connections" {
			return text, api.ConnectionsOnly, "your connections only", opts, nil
		}
		return text, api.Anyone, "anyone", opts, nil
	}
	return []mcp.Tool{
		{
			Name:  "publish_post",
			Title: "Publish a post",
			Description: "Publish a post on the person's LinkedIn, optionally with images, a video, a PDF document, a poll or a link card " +
				"(one of these), or scheduled for later. Files are paths on this computer. Show the person the exact text and " +
				"attachments and get their agreement first. Text with unfilled [placeholders] is refused.",
			Params: []mcp.Param{
				{Name: "text", Type: "string", Description: fmt.Sprintf("the post, at most %d characters", a.cfg.MaxPostChars), Required: true},
				{Name: "visibility", Type: "string", Description: "who can see it", Enum: []string{"anyone", "connections"}, Default: "anyone"},
				{Name: "images", Type: "string", Description: fmt.Sprintf("comma-separated paths of up to %d images (png, jpg, gif, webp)", api.MaxImages)},
				{Name: "alt_texts", Type: "string", Description: "descriptions of the images for screen readers, separated by |, in the same order"},
				{Name: "video", Type: "string", Description: "path of a video (mp4, mov)"},
				{Name: "document", Type: "string", Description: "path of a PDF document"},
				{Name: "document_title", Type: "string", Description: "the document's title, required with document"},
				{Name: "poll_question", Type: "string", Description: fmt.Sprintf("add a poll with this question (up to %d characters)", api.MaxPollQuestion)},
				{Name: "poll_options", Type: "string", Description: fmt.Sprintf("2 to %d poll options separated by |, each up to %d characters", api.MaxPollOptions, api.MaxPollOption)},
				{Name: "poll_duration", Type: "string", Description: "how long the poll runs", Enum: api.PollDurations, Default: "1w"},
				{Name: "link_preview", Type: "boolean", Description: "show the card of the first link in the text", Default: false},
				{Name: "schedule_at", Type: "string", Description: `publish later, at this local time ("2026-11-05 09:30") or an RFC 3339 time; 10 minutes to 3 months ahead`},
			},
			Confirm: func(args mcp.Args) (string, error) {
				text, _, audience, opts, err := checkedPost(args)
				question := fmt.Sprintf("Publish this post on LinkedIn, visible to %s?\n\n%s", audience, text)
				if more := describeExtras(text, opts); more != "" {
					question += "\n\n" + more
				}
				return question, err
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				text, visibility, _, opts, err := checkedPost(args)
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.PublishedPost, error) { return api.Publish(ctx, c, text, visibility, opts) })
			},
		},
		{
			Name:        "edit_post",
			Title:       "Edit a post",
			Description: "Replace the text of one of the person's posts. The whole text is replaced; media stay.",
			Params: []mcp.Param{postParam,
				{Name: "text", Type: "string", Required: true, Description: "the new text of the post"}},
			Confirm: func(args mcp.Args) (string, error) {
				return "Replace the text of your LinkedIn post " + args.String("post") + " with:\n\n" + args.String("text"), nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				urn, err := api.ParsePost(args.String("post"))
				if err != nil {
					return nil, err
				}
				text := postgen.Normalize(args.String("text"))
				if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
					return nil, err
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.EditPost(ctx, c, urn, text) }); err != nil {
					return nil, err
				}
				return map[string]any{"post": urn, "edited": true}, nil
			},
		},
	}
}

// splitList splits a list argument and drops empty items.
func splitList(value, separator string) []string {
	var items []string
	for _, item := range strings.Split(value, separator) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
