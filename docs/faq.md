# FAQ

**Jump to:** [Basics](#basics) · [Safety](#safety) · [Sessions](#sessions) · [Features](#features)

## Basics

### What is lmw?

A command-line client for your own LinkedIn account: read your profile and
feed, write and publish posts, and explore LinkedIn's internal API. It is a
single program with no dependencies.

### Does it use LinkedIn's official API?

No. The official API only lets ordinary developers do very little (basically
post on your own behalf). `lmw` uses the internal API that LinkedIn's website
uses, called Voyager, which gives access to everything the website shows.

### Is it allowed?

No. LinkedIn's User Agreement forbids accessing the service with unofficial
software. `lmw` keeps usage light to reduce the risk of restrictions, but the
risk is never zero. Use it on your own account and at your own risk.

### Which systems does it run on?

Linux and Windows have ready-made binaries. macOS works if you build it with
Go (`go install ...`, see [Getting started](getting-started.md)).

### Does it need a browser, Python or anything else?

No. It is one static file. You only need a browser once, to log in to
LinkedIn and import the session.

## Safety

### Can my account get banned?

It can get restricted, like with any unofficial tool. Community reports say
restrictions mostly follow high volume, fast or regular request patterns, and
traffic that doesn't look like a browser; they are usually temporary (a
security check). `lmw` addresses all three. Read [Stay safe](guides/stay-safe.md).

### Why is it slow?

On purpose. `lmw` waits a random 2-6 seconds between requests, sometimes
longer, so its activity looks like a person browsing. A single request after
a break is immediate.

### Does lmw know my password?

No. It never asks for it and never logs in. It reuses the session your browser
already has.

### Where is my session stored, and who can read it?

In your system's keyring, or in a file only your user can read if there is no
keyring. See [Configuration](configuration.md#files-on-disk). Anyone who can log
in as your user on your computer could read it, as with your browser's
cookies.

### What does lmw send to LinkedIn?

Only the API requests for the command you run, with your session cookies and
browser-like headers. Nothing is sent anywhere else. `lmw -v` shows each request.

## Sessions

### How long does a session last?

Usually months. It ends when you log out in the browser, change your password,
or LinkedIn ends it. Then log in again in the browser and run `lmw auth import`.

### Can I keep using LinkedIn in my browser at the same time?

Yes. `lmw` uses a copy of the same session; both keep working.

### Can I use several accounts?

One at a time. Importing a session replaces the previous one.

### Why doesn't lmw just log in with my email and password?

Logins from an unknown client are the most common trigger for security checks,
and those checks (captchas, codes) need a browser anyway. Reusing your
browser's session avoids both.

## Features

### Can it send messages, read notifications or search people?

Yes: `lmw messages`, `lmw notifications`, `lmw search`, `lmw profile`,
`lmw invitations` and `lmw connect`. See
[People, messages and your network](guides/people-and-messages.md). Bulk
features (mass messages, invitations, scraping search results) will not be
added, because they are what gets accounts restricted.

### Can I react to or comment on posts?

Yes: `lmw post react` and `lmw post comment`, after you confirm. See
[React, comment and search](guides/posts-and-search.md).

### Can an AI assistant use it?

Yes. `lmw mcp` lets assistants such as Claude Code use `lmw` through the Model
Context Protocol, read-only by default. See
[Use lmw from an AI assistant](guides/ai-assistants.md).

### Can it schedule posts?

No. You can call `lmw post publish` from your own scheduler, but it asks for
confirmation by design. Publishing is something you should see happen.

### Why does `post draft` give me an outline instead of a finished post?

Because it works offline and doesn't know your topic. Generic filler text is
easy to spot on LinkedIn; a good structure plus your own words is not.

### LinkedIn changed something and a command broke. What now?

If the error mentions a GraphQL query, record a HAR on the page that shows the
same data and run `lmw har learn FILE`: `lmw` picks up the version the website
uses now. Otherwise see [Extend lmw](guides/extend-lmw.md): record what the
website does, compare, and fix the endpoint or parser. `lmw api get` and
`lmw har inspect` make this quick.
