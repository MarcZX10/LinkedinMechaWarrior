# lmw documentation

![LinkedinMechaWarrior: pilot your own LinkedIn from the terminal](assets/banner.svg)

`lmw` (**L**inkedin**M**echa**W**arrior) is a command-line client for your own
LinkedIn account: read your feed, profiles, messages and notifications, post,
comment, react, message and connect after you confirm, search people,
companies and jobs, and let an AI assistant do it for you over MCP. This is
the pilot's manual, from the first command to building new ones.

> [!TIP]
> New here? Read [Getting started](getting-started.md). It takes about ten
> minutes and ends with your feed in the terminal.

## Pick your mission

<div class="grid cards" markdown>

-   🚀 **[Getting started](getting-started.md)**

    ---

    Install `lmw`, connect your account and read your feed. About ten minutes,
    from zero to the cockpit.

-   🔌 **[Connect your account](guides/import-session.md)**

    ---

    Reuse your browser's LinkedIn session, pick the best method for your
    browser, or switch accounts.

-   📡 **[Read LinkedIn](guides/read-linkedin.md)**

    ---

    Your profile and feed, JSON output, and recipes that combine `lmw` with
    other tools.

-   💬 **[People, messages and your network](guides/people-and-messages.md)**

    ---

    Profiles, your inbox, sending messages, invitations, connections and
    notifications.

-   🎯 **[React, comment and search](guides/posts-and-search.md)**

    ---

    A post's comments and reactions, reacting and commenting, searching people,
    companies and jobs.

-   ✍️ **[Write and publish posts](guides/write-posts.md)**

    ---

    From an idea to a published post, with an outline, a preview and a final
    confirmation.

-   🤖 **[Use lmw from an AI assistant](guides/ai-assistants.md)**

    ---

    Dock `lmw` into Claude Code or any MCP client, read-only or with your
    approval for every change.

-   🛡️ **[Stay safe](guides/stay-safe.md)**

    ---

    Pacing, budgets and cooldowns, and what to do if LinkedIn pushes back.

-   🥷 **[Browser identity](guides/browser-identity.md)**

    ---

    Which browser `lmw` looks like, down to the TLS fingerprint and header
    order.

-   🔧 **[Extend lmw](guides/extend-lmw.md)**

    ---

    Find out how LinkedIn's website gets some data and turn it into a new
    command.

</div>

## Look things up

| Page | Contents |
|---|---|
| [Command reference](reference/lmw.md) | Every command and flag, generated from the code. |
| [Configuration](configuration.md) | Environment variables, files on disk, exit codes. |
| [Troubleshooting](troubleshooting.md) | Every error message, what it means and how to fix it. |
| [FAQ](faq.md) | Short answers to common questions. |
| [Glossary](glossary.md) | What words like *session cookie*, *HAR*, *cooldown* or *MCP* mean here. |
| [How it works](how-it-works.md) | Architecture, the life of a request, GraphQL and the MCP server, with diagrams. |

## The commands you will use most

```bash
lmw auth import --har linkedin.har      # connect your account (once)
lmw me                                  # who am I logged in as?
lmw feed -n 20                          # read the feed
lmw messages                            # your latest conversations
lmw unread                              # how many unread notifications and messages
lmw notifications                       # what happened lately
lmw search people grace hopper          # find someone
lmw post publish --text-file post.txt   # publish a post (asks first)
lmw limits                              # how many requests have I made?
```

> [!WARNING]
> `lmw` uses LinkedIn's internal web API, which goes against LinkedIn's User
> Agreement. It is designed to stay light and human-paced, but using it can
> still get your account restricted. Read [Stay safe](guides/stay-safe.md)
> before relying on it.
