# People, messages and your network

> **Short version:** `lmw profile show NAME` reads a profile, `lmw messages`
> shows your inbox, `lmw invitations` your connection requests, and
> `lmw notifications` what happened lately. Everything that changes something
> asks you to confirm first.

Wherever a command asks for a `PROFILE`, you can paste the profile's address
(`https://www.linkedin.com/in/ada-lovelace/`), or write just the name in it
(`ada-lovelace`).

## Profiles

```bash
lmw profile show ada-lovelace
```

```text
Ada Lovelace
Mathematician · Writing the first program for the Analytical Engine
London, England · Research
https://www.linkedin.com/in/ada-lovelace/

About
  I like engines that weave algebraic patterns.

Experience
  Analyst · Analytical Engines
    1842-07 – present · London

Education
  Home schooling
    Mathematics · 1830 – 1835

Skills
  Algorithms, Mathematics, Poetry
```

| Command | Shows | Requests |
|---|---|---|
| `lmw profile show PROFILE` | Headline, location, about, experience, education, skills | 1 |
| `lmw profile contact PROFILE` | The contact info the member shares with you (usually only with connections) | 2 |
| `lmw profile posts PROFILE -n 5` | Their latest posts, like `lmw feed` | 2 or more |
| `lmw profile follow PROFILE` | Follow them (asks for confirmation) | 2 |
| `lmw profile unfollow PROFILE` | Stop following them | 2 |

## Messages

```bash
lmw messages                     # latest 10 conversations
lmw messages --folder requests   # message requests from people you don't know
```

```text
• Grace Hopper · 2026-11-02   id 2-ZTU4MDlhMDYtM2QyOC00YjM2LWE4NGYtZWIyOGQyNjE0ZTA0XzEwMA==
  Grace Hopper: See you at the conference!

  Alan Turing · 2026-10-30   id 2-OTk5ZGIzYzAtZTJmMy00ZjU0LWJhMmQtYzE2ZjY3YmVjYjI5XzEwMA==
  Ada Lovelace: Thanks for the paper.
```

The dot marks unread conversations. Use the id, or the conversation's
address from your browser, with the other commands:

```bash
lmw messages show 2-ZTU4MDlhMDYtM2QyOC00YjM2LWE4NGYtZWIyOGQyNjE0ZTA0XzEwMA==
lmw messages send --conversation 2-ZTU4MDlhMDYtM2QyOC00YjM2LWE4NGYtZWIyOGQyNjE0ZTA0XzEwMA== --text "Me too!"
lmw messages send --to ada-lovelace --text-file note.txt
lmw messages read 2-ZTU4MDlhMDYtM2QyOC00YjM2LWE4NGYtZWIyOGQyNjE0ZTA0XzEwMA==
```

- `messages show` doesn't mark the conversation as read; `messages read`
  does, and the other side may see it.
- `messages send` shows the message and waits until you type `send`.
- Folders: `inbox` (the default), `other`, `archive`, `requests`, `spam`.

> [!NOTE]
> LinkedIn only lets you message your connections, and other people when your
> account or theirs allows it. Otherwise it answers with an error that
> `lmw` shows as it comes.

## Invitations and connections

```bash
lmw invitations              # requests you received
lmw invitations sent         # requests you sent that are still pending
```

```text
Grace Hopper · 2026-11-01   id 7123456789012345678
  Rear Admiral · Computer scientist
  “Loved your notes on the Analytical Engine.”
  https://www.linkedin.com/in/grace-hopper/
```

```bash
lmw invitations accept 7123456789012345678
lmw invitations ignore 7123456789012345678
lmw invitations withdraw 7234567890123456789
lmw connect ada-lovelace --note "We met at the Royal Society."
lmw connections -n 20
lmw connections remove ada-lovelace
```

- A note is optional and can be up to 300 characters. Without `--note`,
  `lmw` sends none, which doesn't use up LinkedIn's limited personalized
  invitations.
- Each of these asks you to type a word (`accept`, `connect`, `remove`...)
  before it changes anything.

## Unread counts

```bash
lmw unread
```

```text
Notifications  3
Messages       1
My network     2
```

These are the badges of LinkedIn's top bar, read with one request, and
reading them marks nothing as seen. Add `--folders` to count unread
conversations in each message folder (two more requests):

```text
Unread conversations: inbox 1, other 0, requests 1, archive 0, spam 0
```

To keep an eye on them, `--watch` counts again every so often and prints a
line when something changes, with what went up. Ctrl-C stops it:

```bash
lmw unread --watch 10m
```

```text
Watching every 10m0s (6 requests an hour). Ctrl-C stops.
18:40  notifications 3 · messages 1 · my network 2
19:10  notifications 4 (+1) · messages 1 · my network 2
```

The watch may use up to 12 requests an hour, a fifth of the hourly budget,
so `lmw` refuses shorter intervals (every 5 minutes at least, or every 15
with `--folders`). It stops at the first error, so if LinkedIn pushes back
the watch ends instead of insisting. With `--json` each change is one line
of JSON.

`--on-change` runs a command of yours in your shell when a count goes up,
for example a desktop notification:

```bash
lmw unread --watch 15m --on-change 'notify-send LinkedIn "$LMW_NEW_MESSAGES new messages"'
```

The command gets the counts in `LMW_NOTIFICATIONS`, `LMW_MESSAGES` and
`LMW_NETWORK`, what went up in `LMW_NEW_NOTIFICATIONS`, `LMW_NEW_MESSAGES`
and `LMW_NEW_NETWORK`, and all of it as JSON in `LMW_UNREAD`. Its output goes
to stderr. If it fails, `lmw` says so and keeps watching.

## Notifications

```bash
lmw notifications -n 20
```

```text
• Grace Hopper commented on your post: "Great write-up!"
  2026-11-02   https://www.linkedin.com/feed/update/urn:li:activity:7123456789012345678/
```

The dot marks unread notifications. Reading them with `lmw` doesn't mark
them as read.

## JSON

All these commands accept `--json`. The lists come out as JSON arrays, so they
combine with other tools:

```bash
lmw invitations --json | jq -r '.[] | "\(.id) \(.name): \(.message // "")"'
```
