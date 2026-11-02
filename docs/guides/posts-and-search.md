# React, comment and search

> **Short version:** `lmw post show POST` reads a post with its comments,
> `lmw post react POST` and `lmw post comment POST --text "..."` take part,
> `lmw search people grace hopper` finds people, and `lmw jobs search` finds
> jobs.

A `POST` can be the post's address, copied from your browser (the *Copy link
to post* option, or the address bar), or its URN such as
`urn:li:activity:7123456789012345678`. `lmw feed --json` shows both.

## Read a post

```bash
lmw post show https://www.linkedin.com/feed/update/urn:li:activity:7123456789012345678/
lmw post comments urn:li:activity:7123456789012345678 -n 20
lmw post reactions urn:li:activity:7123456789012345678
```

`post show` prints the post like `lmw feed` does, followed by the 5 most
relevant comments (`--comments 0` to leave them out).

## Take part

```bash
lmw post react urn:li:activity:7123456789012345678 --type celebrate
lmw post unreact urn:li:activity:7123456789012345678
lmw post comment urn:li:activity:7123456789012345678 --text "Thanks for sharing this."
lmw post delete urn:li:activity:7123456789012345678
```

- Reactions: `like`, `celebrate`, `support`, `love`, `insightful`, `funny`.
- `post comments` shows each comment's URN, e.g.
  `urn:li:fsd_comment:(7001,urn:li:activity:7123456789012345678)`. Give it to
  `post reply` to answer it, or to `post react` and `post unreact`:

```bash
lmw post reply "urn:li:fsd_comment:(7001,urn:li:activity:7123456789012345678)" --text "Thanks!"
lmw post react "urn:li:fsd_comment:(7001,urn:li:activity:7123456789012345678)" --type insightful
```

- Comments can be up to 1250 characters; `--text-file` reads one from a file.
- Each command shows what it will do and waits until you type its name
  (`react`, `comment`, `delete`...).

> [!WARNING]
> LinkedIn sometimes reports an error *after* a comment was created. `lmw`
> never retries; if a comment fails, look at the post before trying again, or
> you may comment twice.

To publish your own posts, see [Write and publish posts](write-posts.md).

## Search

```bash
lmw search people grace hopper
lmw search people "go developer" --network 2nd -n 20
lmw search companies analytical engines
lmw search schools oxford
```

```text
Grace Hopper
  Rear Admiral · Computer scientist · New York
  https://www.linkedin.com/in/grace-hopper/
```

- `--network` keeps people of some connection degrees: `1st`, `2nd`, `3rd`
  (comma-separated).
- Results come 10 at a time, like on the website; `-n` up to 50.
- Posts can't be searched: LinkedIn's website serves that search in a format
  `lmw` doesn't read.

## Companies and schools

```bash
lmw company show https://www.linkedin.com/company/anthropic/
lmw company follow anthropic
lmw company unfollow anthropic
```

## Jobs

```bash
lmw jobs search go developer --location Barcelona
lmw jobs search "data engineer" --workplace remote,hybrid --posted week -n 20
lmw jobs show https://www.linkedin.com/jobs/view/4012345678/
```

```text
Go engineer   id 4012345678
  Acme · Barcelona, Catalonia, Spain (Hybrid)
  https://www.linkedin.com/jobs/view/4012345678/
```

- `--workplace`: `onsite`, `remote`, `hybrid`.
- `--posted`: `day`, `week`, `month`.
- `jobs show` prints the description (`--full` for all of it) and the link to
  apply. Applying is done on the website.

## When a command stops working

Comments, reactions, messages, search and members' posts are read through
LinkedIn's GraphQL queries, whose version changes when LinkedIn updates its
website. `lmw` knows several versions and tries the next when one is
rejected. If none works, the error says so: record a HAR on the page that
shows the same data (the post's comments, your inbox, a search...) and run:

```bash
lmw har learn linkedin.har
```

`lmw` keeps the versions the website uses and tries them first from then on.
See [Extend lmw](extend-lmw.md) for how to record a HAR.
