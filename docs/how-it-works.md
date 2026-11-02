# How it works

A tour of `lmw`'s design: what happens when you run a command, and why each
piece exists.

## The big picture

```mermaid
flowchart TB
    you([You]) --> cli["CLI<br/>internal/cli"]
    browser([Your browser]) -. "HAR / cookies<br/>(once)" .-> cli
    cli --> api["Features<br/>internal/api"]
    api --> client["Voyager client<br/>internal/voyager"]
    client --> pacer["Pacer<br/>internal/pacing"]
    client --> id["Identity<br/>internal/identity"]
    client --> tls["tls-client<br/>Chrome TLS + HTTP/2"]
    tls ==> li[(LinkedIn<br/>/voyager/api)]
    cli <--> store[("Session store<br/>keyring or file")]
    pacer <--> log[("request-log.json")]
```

| Piece | Job |
|---|---|
| **CLI** | Parses the command, loads the session and identity, prints results. |
| **Features** (`api`) | Knows which endpoint to call and how to read the answer. |
| **Voyager client** | Builds the request (URL, headers, cookies), sends it and interprets LinkedIn's answer, including every sign of trouble. |
| **Pacer** | Decides whether a request may be sent and when: pauses, budgets, cooldowns. |
| **Identity** | Which browser the requests claim to come from. |
| **tls-client** | Opens the connection with the same TLS and HTTP/2 fingerprint as that browser. |
| **Session store** | Keeps the session cookies in the system keyring, or a private file. |

## The life of `lmw feed`

```mermaid
sequenceDiagram
    autonumber
    actor You
    participant CLI as CLI
    participant Store as Session store
    participant Client as Voyager client
    participant Pacer
    participant LI as LinkedIn
    You->>CLI: lmw feed -n 20
    CLI->>Store: load session
    Store-->>CLI: li_at, JSESSIONID, ...
    loop one page of 10 posts, twice
        CLI->>Client: GET /feed/updatesV2?count=10&q=chronFeed&start=N
        Client->>Pacer: may I send?
        Pacer->>Pacer: cooldown? budgets?
        Pacer-->>Client: yes, after a random pause
        Client->>LI: request with cookies, csrf-token, browser headers
        LI-->>Client: normalized JSON (+ maybe new cookies)
        Client->>Client: check status, redirects, cookies
        Client-->>CLI: JSON
        CLI->>CLI: parse posts
    end
    CLI->>Store: save refreshed cookies (if any)
    CLI-->>You: formatted posts
```

## Looking like a browser

LinkedIn can compare several layers of every request. `lmw` keeps them
consistent:

1. **Connection.** [tls-client](https://github.com/bogdanfinn/tls-client)
   reproduces a browser's TLS handshake and HTTP/2 settings. Measured with a
   fingerprinting service, `lmw`'s JA4 (`t13d1517h2_8daaf6152771_...`) and
   HTTP/2 fingerprint are identical to a real Chromium's.
2. **Browser headers.** User-Agent, `sec-ch-ua` (built with Chrome's own
   algorithm for its fake "GREASE" brand), `sec-ch-ua-platform`,
   `sec-fetch-*`, `accept-language`, and `priority`, in a browser-like order.
3. **LinkedIn's headers.** `csrf-token` (the `JSESSIONID` without quotes),
   `x-restli-protocol-version: 2.0.0`, `x-li-lang`, and `x-li-track` when
   imported from a HAR, plus the `accept` header that asks for normalized JSON.
4. **Session.** The cookies come from your real browser session, and cookies
   LinkedIn refreshes are kept, as a browser would.

Importing a HAR replaces the defaults with your own browser's exact values,
including header order. See [Browser identity](guides/browser-identity.md).

## How LinkedIn's answers are interpreted

| LinkedIn answers | `lmw` concludes | Then |
|---|---|---|
| Redirect to `/checkpoint` or `/challenge` | Security check | 6-hour cooldown, stop |
| HTTP 403 mentioning a challenge | Security check | 6-hour cooldown, stop |
| HTTP 429 or 999 | Too many requests | 1-hour cooldown, stop |
| Redirect to `/login`, `/authwall`, `/uas/`, `/signup`, or HTTP 401 | Session expired | Stop, ask to import again |
| HTTP 403 mentioning CSRF | Session token mismatch | Stop, ask to import again |
| `Set-Cookie` deleting `li_at` | LinkedIn ended the session | Remove stored session, stop |
| Other `3xx`/`4xx`/`5xx` | Unexpected | Stop, show status and the start of the answer |
| `2xx` with HTML or invalid JSON | Unexpected page | Stop, show what came back |
| `2xx` with JSON | Success | Parse it |

`lmw` never retries automatically. A failed request is shown to you; repeating
it is your decision.

## Pacing in detail

```mermaid
stateDiagram-v2
    [*] --> Ready
    Ready --> Waiting: request wanted
    Waiting --> Sending: random pause done
    Sending --> Ready: OK
    Ready --> OverBudget: 60/h or 300/24h reached
    OverBudget --> Ready: old requests leave the window
    Sending --> Cooldown: rate limited (1 h) or challenge (6 h)
    Cooldown --> Ready: time passes, or limits --clear-cooldown
```

- The pause is measured from the **previous request**, which is read from
  `request-log.json`, so separate commands run back to back are paced too.
- The pause is random between the minimum and maximum (2-6 s), and one time in
  ten it adds an extra 1-3× the maximum, like someone stopping to read.
- Requests are counted **before** they are sent, so failed requests count too.
- The log only keeps the last 24 hours.

## Sessions and storage

- `auth import` keeps only the cookies the website itself needs for API calls:
  `li_at` (the session), `JSESSIONID` (also the CSRF token), and `bcookie`,
  `bscookie`, `li_gc`, `lang`, `liap`, `lidc` (browser and routing cookies).
  Values are kept byte for byte, including the quotes LinkedIn uses.
- The session goes to the system keyring through
  [go-keyring](https://github.com/zalando/go-keyring), or to a `0600` file if no
  keyring is available.
- Cookie stores of installed browsers are read with
  [kooky](https://github.com/browserutils/kooky).

## GraphQL queries and server-driven pages

Most of the website no longer reads plain REST endpoints:

- **GraphQL queries** serve comments, reactions, search, messages and members'
  posts. Each is named by a `queryId` such as
  `voyagerSearchDashClusters.b0928897…`, whose hash changes when LinkedIn
  deploys a new version of the page. `lmw` keeps the hashes known to work for
  each query and tries the next one when LinkedIn answers HTTP 400. Hashes
  learned from a HAR with `lmw har learn` are tried first. They are matched by
  query name *and* the variables sent, because some queries share a name (the
  same `voyagerSocialDashReactions` lists, adds and removes reactions).
- **Server-driven UI** (`/flagship-web/rsc-action/actions/...`) serves a few
  pages, like contact info, as a React Server Components stream instead of
  JSON. `lmw` posts the same request the website does and reads the fields it
  needs out of the stream.

## The MCP server

`lmw mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io/)
over stdin and stdout (JSON-RPC 2.0, one message per line), so AI assistants
can call `lmw`'s operations as tools. It is part of the same binary and calls
the same code as the commands, through the same session, pacing, budgets and
cooldowns.

- Tools that change something on LinkedIn are only listed when the server is
  started with `--allow-write`.
- When the client supports *elicitation*, `lmw` sends the person the exact
  text that will be posted or sent and waits for them to accept before doing
  anything.
- Calls are handled one at a time; a cancellation from the client stops the
  request in flight.

See [Use lmw from an AI assistant](guides/ai-assistants.md).

## Testing

`go test ./...` never contacts LinkedIn:

- **Fake transports** answer by API path, to test commands end to end:
  importing, reading, publishing, refreshed and deleted cookies, rate limits
  and cooldowns.
- **A local HTTPS server** checks the real tls-client stack: HTTP/2,
  compression and cookies.
- **A real Firefox cookie database** (fixture) checks importing from a browser.
- **Documentation checks** make sure the [command reference](reference/lmw.md)
  matches the code and that every `lmw ...` example in these pages uses
  commands and flags that exist.

## What it deliberately doesn't do

- No browser automation, and no logging in.
- No automatic retries after errors.
- No bulk operations.
- No telemetry: the only network traffic is to LinkedIn, for the command you run.
