package cli

import (
	"github.com/spf13/cobra"

	"github.com/MarcZX100/LinkedinMechaWarrior/internal/mcp"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/version"
	"github.com/MarcZX100/LinkedinMechaWarrior/internal/voyager"
)

const mcpInstructions = `lmw reads and acts on the person's own LinkedIn account through LinkedIn's internal web API, using the session they imported with ` + "`lmw auth import`" + `.

- Every request is paced like a person (a few seconds apart) and counted against hourly and daily budgets, so calls are slow and limited. Ask for what you need, not more: no bulk reading, no loops over many profiles.
- If a tool reports a cooldown, a security check or an expired session, stop and tell the person; don't retry.
- Profiles, posts, companies and jobs can be given as their LinkedIn address or as the id inside it.
- Tools that change something on LinkedIn (publishing, messaging, reacting, connecting) are only available when the person allowed it, and they may be asked to confirm each one. Show the person what you are about to post or send before calling them.`

func (a *app) mcpCommand() *cobra.Command {
	var allowWrite bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Let an AI assistant use lmw through MCP (stdio)",
		Long: `Run lmw as a Model Context Protocol (MCP) server on stdin and stdout, so an
AI assistant such as Claude Code can read your LinkedIn and, if you allow it,
act on it.

By default only tools that read are offered. With --allow-write the assistant
can also publish, comment, react, send messages and handle invitations; your
MCP client asks you to approve each call, and clients that support it also
show you exactly what will be posted or sent and wait for you to accept.

Requests go through the same pacing, budgets and cooldowns as the commands.`,
		Example: `  # Add it to Claude Code (read-only)
  claude mcp add linkedin -- lmw mcp

  # Also allow posting, messaging and other changes
  claude mcp add linkedin -- lmw mcp --allow-write`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			server := &mcp.Server{
				Name:         "lmw",
				Title:        "LinkedIn (lmw)",
				Version:      version.Version,
				Instructions: mcpInstructions,
				Tools:        a.mcpTools(),
				AllowWrite:   allowWrite,
			}
			return server.Serve(cmd.Context(), a.stdin, a.stdout)
		},
	}
	cmd.Flags().BoolVar(&allowWrite, "allow-write", false, "also offer tools that change something on LinkedIn")
	return cmd
}

// fetch runs one operation with the stored session and returns its result.
func fetch[T any](a *app, fn func(*voyager.Client) (T, error)) (T, error) {
	var result T
	err := a.withClient(func(c *voyager.Client) (err error) {
		result, err = fn(c)
		return err
	})
	return result, err
}

// limitParam is the usual "how many" argument of tools that list things.
func limitParam(what string, fallback, max int) mcp.Param {
	return mcp.Param{Name: "limit", Type: "integer", Description: "how many " + what + " to return", Default: fallback, Min: 1, Max: max}
}

// listOf wraps a list result in an object, as MCP clients expect.
func listOf[T any](key string, items []T, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []T{}
	}
	return map[string]any{key: items}, nil
}

func (a *app) mcpTools() []mcp.Tool {
	var tools []mcp.Tool
	for _, group := range [][]mcp.Tool{a.mcpAccountTools(), a.mcpProfileTools(), a.mcpPostTools()} {
		tools = append(tools, group...)
	}
	return tools
}
