# Glossary

The words that appear in `lmw`'s messages and documentation, in plain terms.

## Budget

The maximum number of requests `lmw` will send: 60 in any hour and 300 in
any 24 hours by default. When a budget is reached, `lmw` refuses to send more
until older requests fall out of the window. See [Stay safe](guides/stay-safe.md).

## Challenge

A security check LinkedIn shows when it suspects automation: a captcha, a
code sent by email, a request to verify your identity. `lmw` cannot solve
challenges; you do it in your browser. When one appears, `lmw` pauses for
6 hours.

## Client hints (`sec-ch-ua`)

Headers in which Chrome-based browsers describe themselves (brand, version,
platform). `lmw` sends the same values as the browser it presents itself as.

## Cooldown

A pause during which `lmw` sends no requests at all, started automatically
when LinkedIn pushes back. It survives between commands. Check it with
`lmw limits`.

## CSRF token

A value LinkedIn's web app sends with every request to prove it comes from
the logged-in page. It is the `JSESSIONID` cookie without quotes; `lmw`
derives it for you.

## Feed

Your LinkedIn home page: posts from your network and suggestions.

## Fingerprint (TLS, HTTP/2)

The way a program opens a secure connection: which ciphers, extensions and
settings it offers, and in which order. Servers can tell a real Chrome from a
script by it. `lmw` reproduces Chrome's fingerprint (its JA4 and HTTP/2
fingerprints are identical to a real Chromium's).

## GraphQL query

How most pages of LinkedIn's website read their data today. Each query is
named by a `queryId` like `messengerConversations.9501074288a12f3ae9e3c7ea243bccbf`:
a name and a version hash that changes when LinkedIn updates the page.
`lmw har learn` picks up the current versions from a [HAR file](#har-file).

## HAR file

"HTTP Archive". A file your browser's developer tools can save, with every
request and response of a page. `lmw` reads HAR files to import your session
and identity, and to show which internal API calls LinkedIn's web app makes.
HAR files contain private data: delete them when you are done.

## Identity

The set of things that say which browser a request comes from: the
User-Agent, client hints, language, header order and TLS fingerprint. See
[Browser identity](guides/browser-identity.md).

## `JSESSIONID`

A LinkedIn cookie, with a value like `"ajax:1234567890123456789"` (quotes
included). Required together with `li_at`.

## Keyring

Your operating system's password store: Windows Credential Manager, macOS
Keychain, or the Secret Service on Linux (GNOME Keyring, KWallet). `lmw`
stores the session there.

## `li_at`

LinkedIn's session cookie. Whoever has it is logged in as you, so treat it
like a password. It usually lasts months, until you log out in the browser
or LinkedIn ends the session.

## MCP

The [Model Context Protocol](https://modelcontextprotocol.io/), which lets AI
assistants call tools. `lmw mcp` offers `lmw`'s operations as MCP tools; see
[Use lmw from an AI assistant](guides/ai-assistants.md).

## Normalized JSON

The response format of LinkedIn's internal API: the main data plus a flat
list of "included" entities that refer to each other by URN. You only meet
it when using `lmw api get` or extending `lmw`.

## Pacing

The random pause `lmw` takes before each request (2 to 6 seconds, sometimes
longer), so that its activity never has a machine-like rhythm.

## Server-driven UI

Pages that LinkedIn's server sends as ready-made UI (a React Server Components
stream) rather than as data. Contact info is one; `lmw` reads the fields it
needs from the stream.

## Session

The proof that you are logged in, made of cookies (`li_at`, `JSESSIONID` and
a few others). `lmw` imports it from your browser and never creates one itself.

## URN

LinkedIn's identifiers, such as `urn:li:activity:7381234567890123456` for a
post. A post URN turns into a link as
`https://www.linkedin.com/feed/update/<urn>/`.

## Voyager

The name of LinkedIn's internal web API, the one its website uses. It is not
public or documented, and it changes without notice.
