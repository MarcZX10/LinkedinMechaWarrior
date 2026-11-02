# Changelog

## Unreleased

### Watch and notify

- `watch add`: keep an eye on someone's next posts, profile changes, new
  comments on a post, new jobs for a search, connection requests, messages,
  notifications or the unread badges. The first check only takes note of
  what is there, so you hear about what comes next; `--once` ends a watch
  after its first notification.
- `daemon` checks the watches as they fall due, 24/7 if you like; `watch run`
  does one round, for cron. Both stop and tell you when LinkedIn pushes back.
- `notify add`: notifications by ntfy, Telegram, Discord, Slack, a webhook or
  a command. Tokens stay in a file only you can read.
- All watches together keep to a share of the request budgets: about 6
  requests an hour by default.
- MCP: `create_watch`, `list_watches`, `delete_watch`, `check_watches`,
  `list_notification_channels` and `send_notification`, offered without
  `--allow-write` since they only change lmw's own settings. "Tell me when
  Grace posts next" now works.
- A guide to running lmw on a server, with a systemd unit and the assistant
  connected over SSH.

### Read

- `company posts`: the latest posts of a company or school page (MCP:
  `get_company_posts`; watch: `company`).
- `profile views`: who viewed your profile lately, with LinkedIn's count of
  views (MCP: `get_profile_views`; watch: `views`).
- `unread`: the unread badges of the top bar, with one request; `--folders`
  counts unread conversations by folder, `--watch` counts again every so
  often, and `--on-change` runs a command when a count goes up.

### Puzzles

- `games queens` and `games sudoku`: type in the day's Queens or Mini Sudoku
  board and get the solution, the next step with its reason (`--hint`), or
  every step (`--steps`), reasoned the way a person would. Offline: lmw
  never plays on LinkedIn. MCP: `solve_queens`, `solve_sudoku`.

### Fixed

- `post comments` and `post reactions` answered nothing for reposts: they
  now read the thread of the original post, as the website does.

## 1.0.0 (2026-11-02)

The first complete release: most of what you do on LinkedIn's website, from
the terminal, plus an MCP server so AI assistants can do it too. Everything
that changes something on LinkedIn still shows what it will do and waits for
you to confirm it.

### Read

- `profile show`, `profile posts`, `profile contact`: anyone's profile with
  experience, education and skills, their posts, the contact info they share.
- `post show`, `post comments`, `post reactions`: a post with its thread and
  who reacted.
- `messages` (inbox and other folders) and `messages show`.
- `notifications`, `invitations` (received and `sent`), `connections`.
- `search people` by name, through the message composer's lookup; companies,
  schools and groups through the search box query.
- `company show`, `jobs search` (location, workplace, posting date),
  `jobs show`, `jobs tracked` (your job tracker), `saved` (your saved posts).

### Write, after confirming

- `post publish` with images and alt text, a video, a PDF document, a poll or
  a link card, now or scheduled with `--at`; `post edit`, `post delete`.
- `post react` and `post unreact`, on posts and comments; `post comment` and
  `post reply`, never retried since LinkedIn may create them and still report
  an error.
- `messages send` to a member or a conversation, `messages read`.
- `connect` with an optional note, `invitations accept/ignore/withdraw`,
  `connections remove`.
- `profile follow/unfollow`, `company follow/unfollow`.

### For AI assistants

- `lmw mcp`: 35 MCP tools over stdio, read-only unless started with
  `--allow-write`. Clients that support elicitation show the exact text to be
  posted or sent and `lmw` waits for the person to accept.

### Keeping up with LinkedIn

- GraphQL queries carry the versions known to work and try the next when one
  is retired. `lmw har learn` learns the versions the website uses today from a
  HAR file, matched by query name and variables; `auth import --har` learns
  them too.
- `lmw har find` finds the HAR files you saved (recent files, the browsers'
  download and save folders, Downloads, Desktop), after asking; `--har` without
  a file and `auto` use the newest.
- `lmw doctor` checks the setup; `--probe` reads a little of every feature to
  show which ones work with your account today, and stops at the first sign of
  pushback.

### Also

- Builds for macOS (Apple silicon and Intel) and Linux ARM, besides Linux and
  Windows x86_64.
- `--json` keeps stdout to JSON only; messages for people go to stderr.
- Commands take the addresses you copy from the browser: profiles, posts,
  companies, jobs, conversations.
- A documentation website with guides for every feature.
- The Go module is `github.com/MarcZX10/LinkedinMechaWarrior`.

### Known limits

- Since November 2026 LinkedIn's search box query answers every search with no
  results for some accounts, which affects company, school and group search,
  2nd and 3rd-degree people filters, `saved` and `jobs tracked`. `lmw` says so
  when they come back empty, and `doctor --probe` checks it.
- Posts can't be searched, and who viewed your profile isn't available: the
  website serves both in a format `lmw` doesn't read.

## 0.3.0 and earlier

Profile, feed, publishing text posts, session import, browser identity, HAR
inspection and pacing. See the git history.
