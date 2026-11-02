# Read LinkedIn

> **Short version:** `lmw me` shows your profile, `lmw feed -n 20` shows your
> feed, and `--json` turns either into data for other tools.

## Your profile

```bash
lmw me
```

```text
Ada Lovelace
Mathematician · Writing the first program for the Analytical Engine
https://www.linkedin.com/in/ada-lovelace/
```

One request. A good first command after importing a session.

## Your feed

```bash
lmw feed            # 10 posts
lmw feed -n 30      # 30 posts (1 to 50)
lmw feed --full     # don't cut long posts
```

Each post shows:

```text
Grace Hopper · 2h                                         ← author · age
Rear Admiral · Computer scientist · Inventor of ...       ← author headline
(Alan Turing likes this)                                  ← why it is in your feed, if any

  The most dangerous phrase in the language is ...        ← the text, first 6 lines
  … (use --full to see everything)

1284 reactions · 96 comments · 41 reposts   https://www.linkedin.com/feed/update/urn:li:activity:.../
```

- **Reposts** show `Reposting <original author>`, with the original text when
  the person reposting added none.
- **Sponsored posts** are marked `promoted`.
- **Links** open the post in your browser, where you can react or comment.

### How many requests does it cost?

LinkedIn's web app loads the feed 10 posts at a time, and so does `lmw`:

| Command | Requests | Typical time |
|---|---|---|
| `lmw feed` | 1 | a second |
| `lmw feed -n 20` | 2 | a few seconds |
| `lmw feed -n 50` | 5 | about 15-30 seconds |

The time comes from the random pauses between requests (see
[Stay safe](stay-safe.md)). It is intentional.

## JSON output

Every command that shows data accepts `--json`: `me`, `feed`, `profile`,
`post show`, `messages`, `invitations`, `connections`, `notifications`,
`search`, `company`, `jobs`, and also `auth status`, `limits`,
`identity show`, `har inspect` and the commands that change something, which
print what they did. It can go before or after the command name.

```bash
lmw me --json
```

```json
{
  "urn": "urn:li:fsd_profile:ACoAAA1",
  "public_id": "ada-lovelace",
  "first_name": "Ada",
  "last_name": "Lovelace",
  "name": "Ada Lovelace",
  "headline": "Mathematician · Writing the first program for the Analytical Engine",
  "url": "https://www.linkedin.com/in/ada-lovelace/"
}
```

```bash
lmw feed -n 1 --json
```

```json
[
  {
    "urn": "urn:li:activity:7381234567890123456",
    "url": "https://www.linkedin.com/feed/update/urn:li:activity:7381234567890123456/",
    "author": "Grace Hopper",
    "author_headline": "Rear Admiral · Computer scientist · Inventor of the first compiler",
    "age": "2h",
    "text": "The most dangerous phrase in the language is ...",
    "reactions": 1284,
    "comments": 96,
    "reposts": 41,
    "promoted": false
  }
]
```

### Feed fields

| Field | Type | Meaning |
|---|---|---|
| `urn` | string | The post's identifier (`urn:li:activity:...`) |
| `url` | string | Link to the post |
| `author` | string | Who posted it |
| `author_headline` | string | Their headline, or follower count for companies |
| `age` | string | As LinkedIn shows it: `2h`, `1d`, `3w`... |
| `text` | string | Full text of the post |
| `context` | string | Why it is in your feed, e.g. `Alan Turing likes this` |
| `reshared_author` | string | For reposts, the original author |
| `reactions`, `comments`, `reposts` | number or `null` | Counts; `null` when LinkedIn didn't include them |
| `promoted` | boolean | Sponsored post |

Empty text fields are left out of the JSON.

## Recipes

These use [jq](https://jqlang.github.io/jq/), a small tool for JSON.

**Authors and links, without ads:**

```bash
lmw feed -n 20 --json | jq -r '.[] | select(.promoted | not) | "\(.author): \(.url)"'
```

**The most popular post in your feed right now:**

```bash
lmw feed -n 30 --json | jq 'max_by(.reactions // 0) | {author, reactions, url}'
```

**Keep a daily snapshot of your feed:**

```bash
lmw feed -n 20 --json > "feed-$(date +%F).json"
```

**Only continue a script if the session works:**

```bash
lmw auth status && lmw feed -n 10 --json > feed.json
```

**PowerShell (Windows) equivalent of the first recipe:**

```powershell
lmw feed -n 20 --json | ConvertFrom-Json | Where-Object { -not $_.promoted } | ForEach-Object { "$($_.author): $($_.url)" }
```

> [!WARNING]
> If you schedule `lmw` (cron, Task Scheduler), keep it to a few runs a day at
> irregular times. A job that fetches the feed every 10 minutes is exactly the
> kind of pattern that gets accounts restricted, budgets or not.

## More to read

- Profiles, messages, invitations, connections and notifications:
  [People, messages and your network](people-and-messages.md).
- Posts with their comments and reactions, search, companies and jobs:
  [React, comment and search](posts-and-search.md).

## Anything else: `api get`

`lmw api get` sends a GET request to any path of LinkedIn's internal API and
prints the JSON. It goes through the same pacing, budgets and safety checks as
every other command.

```bash
lmw api get /me
lmw api get /feed/updatesV2 -p count=10 -p q=chronFeed -p start=0
```

It is mainly a tool for exploring and for building new commands; see
[Extend lmw](extend-lmw.md).
