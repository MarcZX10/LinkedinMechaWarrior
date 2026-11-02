package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

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
		a.postReactCommand(), a.postUnreactCommand(), a.postCommentCommand(), a.postDeleteCommand())
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

func (a *app) postPublishCommand() *cobra.Command {
	var source textSource
	var connectionsOnly bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a text post (asks for confirmation)",
		Long: "Publish a text post after showing it and asking you to type 'publish'.\n\n" +
			"If LinkedIn rejects the request, its API for creating posts may have changed:\n" +
			"publish one post from the website while recording a HAR, then check it with\n" +
			"`lmw har inspect FILE --filter contentcreation`.",
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			text, err := source.read()
			if err != nil {
				return err
			}
			if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
				return err
			}
			audience, visibility := "anyone", api.Anyone
			if connectionsOnly {
				audience, visibility = "your connections only", api.ConnectionsOnly
			}
			a.note("%s\nVisible to: %s\n", output.PostPreview(text), audience)
			ok, err := a.confirm("Type 'publish' to publish this post on LinkedIn: ", "publish")
			if err != nil || !ok {
				a.note("Cancelled; nothing was published.\n")
				return err
			}

			var post api.PublishedPost
			err = a.withClient(func(c *voyager.Client) (err error) {
				post, err = api.Publish(cmd.Context(), c, text, visibility)
				return err
			})
			if err != nil {
				return err
			}
			if a.jsonOut {
				return output.JSON(a.stdout, post)
			}
			if post.URL != "" {
				a.printf("Published: %s\n", post.URL)
			} else {
				a.printf("Published. LinkedIn did not return the post's address; check your profile.\n")
			}
			return nil
		},
	}
	source.register(cmd)
	cmd.Flags().BoolVar(&connectionsOnly, "connections-only", false, "only your connections can see the post")
	return cmd
}

func (a *app) mcpPostTools() []mcp.Tool {
	// checkedPost normalizes and validates the text the assistant wants to publish.
	checkedPost := func(args mcp.Args) (string, api.Visibility, string, error) {
		text := postgen.Normalize(args.String("text"))
		if err := postgen.Validate(text, a.cfg.MaxPostChars); err != nil {
			return "", 0, "", err
		}
		if args.String("visibility") == "connections" {
			return text, api.ConnectionsOnly, "your connections only", nil
		}
		return text, api.Anyone, "anyone", nil
	}
	return []mcp.Tool{
		{
			Name:  "publish_post",
			Title: "Publish a post",
			Description: "Publish a text post on the person's LinkedIn. Show the person the exact text and get their agreement first. " +
				"Text with unfilled [placeholders] is refused.",
			Params: []mcp.Param{
				{Name: "text", Type: "string", Description: fmt.Sprintf("the post, at most %d characters", a.cfg.MaxPostChars), Required: true},
				{Name: "visibility", Type: "string", Description: "who can see it", Enum: []string{"anyone", "connections"}, Default: "anyone"},
			},
			Confirm: func(args mcp.Args) (string, error) {
				text, _, audience, err := checkedPost(args)
				return fmt.Sprintf("Publish this post on LinkedIn, visible to %s?\n\n%s", audience, text), err
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				text, visibility, _, err := checkedPost(args)
				if err != nil {
					return nil, err
				}
				return fetch(a, func(c *voyager.Client) (api.PublishedPost, error) { return api.Publish(ctx, c, text, visibility) })
			},
		},
	}
}
