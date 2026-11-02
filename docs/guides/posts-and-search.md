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

- People are found by name with the lookup of the message composer, which
  returns up to about 10 matches. `--network 1st` keeps only your connections.
- `--network 2nd` or `3rd`, and companies, schools and groups, use the search
  box query, 10 results at a time with `-n` up to 50.
- Posts can't be searched: LinkedIn's website serves that search in a format
  `lmw` doesn't read.

> [!WARNING]
> On 2 November 2026 LinkedIn answered every search box query with no results,
> which is why people are now found through the composer's lookup. Companies,
> schools, groups, `--network 2nd`/`3rd`, saved posts and the job tracker still
> go through the search box query and may come back empty. If a search you know
> should match finds nothing, `lmw doctor --probe` tells you whether search
> works for your account. A HAR of the website won't show a fix: its search
> results page comes ready made from LinkedIn's servers and calls no API
> while you search.

## Companies and schools

```bash
lmw company show https://www.linkedin.com/company/anthropic/
lmw company posts anthropic -n 5
lmw company follow anthropic
lmw company unfollow anthropic
```

`company posts` shows the latest posts of the page, as its Posts tab does,
with two requests: one to find the page, one for its posts. To hear about
new ones, add a [watch](watches.md): `lmw watch add company anthropic`.

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

Your job tracker (the jobs you saved or applied to) has one list per stage:

```bash
lmw jobs tracked                    # saved jobs
lmw jobs tracked --stage applied    # also: in-progress, interview, archived
```

## Saved posts

```bash
lmw saved -n 20
```

The posts you saved with *Save* on the website, most recently saved first,
with their links.

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
