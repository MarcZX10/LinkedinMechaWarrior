package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/output"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

func folderNames() []string {
	names := make([]string, 0, len(api.MessageFolders))
	for name := range api.MessageFolders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a *app) messagesCommand() *cobra.Command {
	var limit int
	var folder string
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Read and send messages",
		Long: "List your latest conversations, newest first; unread ones are marked with a dot.\n" +
			"The subcommands show a conversation, send a message and mark one as read.\n\n" +
			"A conversation is named by the id shown in the list or by its address\n" +
			"(linkedin.com/messaging/thread/ID/).",
		Example: "  lmw messages\n" +
			"  lmw messages show 2-ZTU4MDlhMDYtM2QyOC00YjM2LWE4NGYtZWIyOGQyNjE0ZTA0XzEwMA==\n" +
			"  lmw messages send --to ada-lovelace --text \"Hi Ada!\"",
		Args: exactArgs(0, "no arguments, or a subcommand such as show"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			conversations, err := fetch(a, func(c *voyager.Client) ([]api.Conversation, error) {
				return api.Conversations(cmd.Context(), c, folder, limit)
			})
			if err != nil {
				return err
			}
			return printList(a, conversations, "No conversations.", output.Conversations)
		},
	}
	limitFlag(cmd, &limit, "conversations", 10, api.MaxListItems)
	cmd.Flags().StringVar(&folder, "folder", "inbox", "folder: "+strings.Join(folderNames(), ", "))
	cmd.AddCommand(a.messagesShowCommand(), a.messagesSendCommand(), a.messagesReadCommand())
	return cmd
}

func (a *app) messagesShowCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "show CONVERSATION",
		Short: "Show the latest messages of a conversation",
		Long:  "Show the latest messages of a conversation, oldest first. This does not mark it as read.",
		Args:  exactArgs(1, "one conversation id, as shown by `lmw messages`"),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := api.ParseConversation(args[0])
			if err != nil {
				return err
			}
			messages, err := fetch(a, func(c *voyager.Client) ([]api.Message, error) { return api.Messages(cmd.Context(), c, id, limit) })
			if err != nil {
				return err
			}
			return printList(a, messages, "No messages.", output.Messages)
		},
	}
	limitFlag(cmd, &limit, "messages", 20, api.MaxListItems)
	return cmd
}

func (a *app) messagesSendCommand() *cobra.Command {
	var to, conversation string
	source := textSource{what: "message"}
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message (asks for confirmation)",
		Long: "Send a message to a member (--to) or to an existing conversation\n" +
			"(--conversation), after showing it and asking you to type 'send'.\n\n" +
			"LinkedIn only lets you message your connections, and others when your\n" +
			"account allows it.",
		Example: "  lmw messages send --to ada-lovelace --text \"Hi Ada!\"\n" +
			"  lmw messages send --conversation 2-ZTU4MDlh... --text-file reply.txt",
		Args: exactArgs(0, "no arguments; use --to or --conversation"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (to == "") == (conversation == "") {
				return errUsage("use either --to PROFILE or --conversation ID")
			}
			text, err := source.read()
			if err != nil {
				return err
			}
			var ref api.ProfileRef
			var id, target string
			if to != "" {
				if ref, err = api.ParseProfile(to); err != nil {
					return err
				}
				target = ref.String()
			} else {
				if id, err = api.ParseConversation(conversation); err != nil {
					return err
				}
				target = "conversation " + id
			}
			if ok, err := a.approve(fmt.Sprintf("Send to %s:\n%s", target, text), "send"); err != nil || !ok {
				return err
			}
			sent, err := fetch(a, func(c *voyager.Client) (string, error) { return api.SendMessage(cmd.Context(), c, id, ref, text) })
			if err != nil {
				return err
			}
			return a.done("Message sent.", map[string]any{"conversation": sent, "sent": true})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "the member to write to (profile address or name)")
	cmd.Flags().StringVar(&conversation, "conversation", "", "the conversation to reply in")
	source.register(cmd)
	return cmd
}

func (a *app) messagesReadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "read CONVERSATION",
		Short: "Mark a conversation as read (asks for confirmation)",
		Long:  "Mark a conversation as read. The other participants may see that you read it.",
		Args:  exactArgs(1, "one conversation id, as shown by `lmw messages`"),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := api.ParseConversation(args[0])
			if err != nil {
				return err
			}
			if ok, err := a.approve("Mark conversation "+id+" as read.", "read"); err != nil || !ok {
				return err
			}
			if err := a.withClient(func(c *voyager.Client) error { return api.MarkRead(cmd.Context(), c, id) }); err != nil {
				return err
			}
			return a.done("Marked as read.", map[string]any{"conversation": id, "read": true})
		},
	}
}

func (a *app) mcpMessagingTools() []mcp.Tool {
	conversationParam := mcp.Param{Name: "conversation", Type: "string", Required: true,
		Description: "the conversation id from list_conversations, or its address"}
	return []mcp.Tool{
		{
			Name:        "list_conversations",
			Title:       "Conversations",
			Description: "The person's latest message conversations, newest first, with participants, last message and unread count.",
			ReadOnly:    true,
			Params: []mcp.Param{
				{Name: "folder", Type: "string", Description: "which folder", Enum: folderNames(), Default: "inbox"},
				limitParam("conversations", 10, api.MaxListItems),
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				items, err := fetch(a, func(c *voyager.Client) ([]api.Conversation, error) {
					return api.Conversations(ctx, c, args.String("folder"), args.Int("limit"))
				})
				return listOf("conversations", items, err)
			},
		},
		{
			Name:        "get_conversation",
			Title:       "Conversation messages",
			Description: "The latest messages of a conversation, oldest first. Does not mark it as read.",
			ReadOnly:    true,
			Params:      []mcp.Param{conversationParam, limitParam("messages", 20, api.MaxListItems)},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				id, err := api.ParseConversation(args.String("conversation"))
				if err != nil {
					return nil, err
				}
				items, err := fetch(a, func(c *voyager.Client) ([]api.Message, error) { return api.Messages(ctx, c, id, args.Int("limit")) })
				return listOf("messages", items, err)
			},
		},
		{
			Name:  "send_message",
			Title: "Send a message",
			Description: "Send a message as the person, to a member (to) or in an existing conversation (conversation); give exactly one. " +
				"Show the person the exact text first. Only for messages the person asked to send.",
			Params: []mcp.Param{
				{Name: "to", Type: "string", Description: "the member's profile address or name, to start or continue a 1:1 conversation"},
				{Name: "conversation", Type: "string", Description: "the conversation id, to reply in it"},
				{Name: "text", Type: "string", Required: true, Description: "the message"},
			},
			Confirm: func(args mcp.Args) (string, error) {
				target := firstNonEmptyString(args.String("to"), "conversation "+args.String("conversation"))
				return "Send this LinkedIn message to " + target + "?\n\n" + args.String("text"), nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				if (args.String("to") == "") == (args.String("conversation") == "") {
					return nil, errUsage("give exactly one of to and conversation")
				}
				var ref api.ProfileRef
				var id string
				var err error
				if args.String("to") != "" {
					ref, err = api.ParseProfile(args.String("to"))
				} else {
					id, err = api.ParseConversation(args.String("conversation"))
				}
				if err != nil {
					return nil, err
				}
				sent, err := fetch(a, func(c *voyager.Client) (string, error) { return api.SendMessage(ctx, c, id, ref, args.String("text")) })
				if err != nil {
					return nil, err
				}
				return map[string]any{"conversation": sent, "sent": true}, nil
			},
		},
		{
			Name:        "mark_conversation_read",
			Title:       "Mark a conversation as read",
			Description: "Mark a conversation as read. The other participants may see it.",
			Params:      []mcp.Param{conversationParam},
			Confirm: func(args mcp.Args) (string, error) {
				return "Mark the LinkedIn conversation " + args.String("conversation") + " as read?", nil
			},
			Run: func(ctx context.Context, args mcp.Args) (any, error) {
				id, err := api.ParseConversation(args.String("conversation"))
				if err != nil {
					return nil, err
				}
				if err := a.withClient(func(c *voyager.Client) error { return api.MarkRead(ctx, c, id) }); err != nil {
					return nil, err
				}
				return map[string]any{"conversation": id, "read": true}, nil
			},
		},
	}
}
