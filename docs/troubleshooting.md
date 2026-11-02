# Troubleshooting

Find the message you got (use your browser's search, <kbd>Ctrl</kbd>+<kbd>F</kbd>)
and follow the fix. Errors start with `lmw:`; parts in `<angle brackets>` vary.

> [!TIP]
> Run the command again with `-v` to see every request `lmw` makes and the
> status LinkedIn answers with: `lmw -v feed`. To see at once which features
> work with your account, run `lmw doctor --probe`: it reads a little of each,
> about a dozen paced requests, and says which ones failed and why.

**Jump to:** [Session](#session) · [Importing](#importing) ·
[LinkedIn pushed back](#linkedin-pushed-back) · [Unexpected answers](#unexpected-answers-from-linkedin) ·
[Posts](#posts) · [Messages, network and interactions](#messages-network-and-interactions) ·
[Keyring and files](#keyring-and-files) ·
[Configuration](#configuration) · [Other situations](#other-situations) ·
[Exit codes](#exit-codes)

## Session

| Message | What it means | Fix |
|---|---|---|
| `no LinkedIn session yet. Run 'lmw auth import' first` | No session stored on this computer. | [Import one](guides/import-session.md). |
| `the LinkedIn session has expired. Log in in your browser and run 'lmw auth import'` | LinkedIn answered "not logged in" (HTTP 401, or a redirect to the login page). | Log in to LinkedIn in your browser, then import again. |
| `LinkedIn ended the session. Log in again in your browser and run 'lmw auth import'` | LinkedIn deleted the session cookie in a response. `lmw` has removed the stored session. | Same as above. |
| `LinkedIn rejected the session token (CSRF). Run 'lmw auth import' again` | The `JSESSIONID` doesn't match the session, usually after pasting cookies from two different moments or browsers. | Import again, taking both cookies from the same browser at the same time. |
| `the LinkedIn session is incomplete: missing <li_at / JSESSIONID>` | One of the two required cookies is empty. | Import again; when pasting, check you copied both values. |

## Importing

| Message | What it means | Fix |
|---|---|---|
| `the session was not saved: <reason>` | The session didn't pass the check with LinkedIn, so it was not stored. The reason is one of the other messages on this page. | Fix the reason. To store it anyway, add `--no-verify`. |
| `no LinkedIn session found in <browser>. ...` | No browser profile had valid `li_at` and `JSESSIONID` cookies for LinkedIn. | Log in to LinkedIn in that browser and **close it** before importing. On Windows with Chrome or Edge, use a HAR or paste the cookies: their cookies can't be read by other programs. |
| `the HAR file has no session cookies. Export it with sensitive data included ...` | The browser saved a sanitized HAR without cookies. | Use *Save all as HAR (with sensitive data)*, or import the session another way and run `lmw identity import FILE` for the identity. |
| `the HAR file has no LinkedIn API requests with headers. ...` | The recording doesn't include calls to `/voyager/api/`. | Keep the Network tab open, **reload** LinkedIn, wait for the feed, then save. |
| `the HAR was recorded with a browser lmw cannot imitate (<user agent>); ...` | The HAR comes from Safari or another browser without a matching TLS profile. | Record the HAR with Chrome, Edge, Brave, Opera or Firefox. The session itself was still imported; only the identity was skipped. |
| `<file> is not a HAR file: <details>` | The file is not valid HAR JSON (incomplete download, wrong file). | Save the HAR again. |
| `no HAR file of LinkedIn found there. ...` | `--har` without a file, or `auto`, found no HAR with LinkedIn traffic in the folders it showed. | Save the HAR to your downloads, or give its path. `lmw har find` lists what it sees. |
| `no HAR file was chosen` | You didn't type `yes` when `lmw` asked to look for HAR files. | Run it again and type `yes`, or give the file's path. |
| `use either --browser or --har, not both` | Two import methods at once. | Pick one. |
| `unexpected argument "<x>"; use --browser NAME, --har FILE or no flags` | A word without a flag, e.g. `lmw auth import firefox`. | Write `lmw auth import --browser firefox`. |

## LinkedIn pushed back

These are the messages to take seriously. See [Stay safe](guides/stay-safe.md#if-linkedin-pushes-back)
for the full procedure.

| Message | What it means | Fix |
|---|---|---|
| `LinkedIn rate-limited the session: HTTP <429/999> (too many requests). Requests are paused for an hour` | Too many requests for LinkedIn's taste. A 1-hour cooldown has started. | Wait. Use LinkedIn in the browser normally. Use `lmw` less afterwards. |
| `LinkedIn answered with a CHALLENGE. Requests are paused. ...` | LinkedIn wants to verify that a person is behind the account. A 6-hour cooldown has started. | Complete the check in your browser, import the session again, and only then `lmw limits --clear-cooldown`. |
| `LinkedIn redirected the request to a security checkpoint. Requests are paused. ...` | Same as above, detected by the redirect. | Same as above. |
| `requests are paused until <HH:MM> because LinkedIn pushed back: <reason>. ...` | A cooldown from an earlier problem is still active. Nothing was sent. | Wait until the time shown, or clear it once the account looks fine in the browser: `lmw limits --clear-cooldown`. |
| `hourly request budget reached (<n> requests in 1h). ...` | `lmw` has sent as many requests as allowed in the last hour. Nothing was sent. | Wait; `lmw limits` shows the usage. Raise `LMW_HOURLY_REQUEST_BUDGET` only with care. |
| `daily request budget reached (<n> requests in 24h). ...` | The same for the last 24 hours. | Wait; raise `LMW_DAILY_REQUEST_BUDGET` only with care. |

## Unexpected answers from LinkedIn

LinkedIn's internal API changes without notice. These usually mean that an
endpoint or a response format changed.

| Message | What it means | Fix |
|---|---|---|
| `LinkedIn API returned HTTP <status> for <url>: <start of the answer>` | LinkedIn rejected the request. `404` or `410`: the endpoint moved. `400`: it expects different parameters or body. `403` without "challenge": not allowed for this account. `500`: a problem on LinkedIn's side. | For `5xx`, retry later. Otherwise follow [Extend lmw](guides/extend-lmw.md) to find what the website calls now. |
| `expected JSON from <url> but got <type>: ...` | LinkedIn returned a web page instead of data, often a login or error page. | Run `lmw auth status`; if the session is fine, the endpoint probably changed. |
| `unexpected response from /me: no profile found. ...` | `/me` answered, but not in the expected format. | Look at it with `lmw api get /me` and see [Extend lmw](guides/extend-lmw.md). |
| `LinkedIn no longer accepts the versions of its <query> query that lmw knows ...` | LinkedIn updated the page that uses this GraphQL query and answers 400 to every version `lmw` tried. | Record a HAR while the matching page loads (your inbox, a post's comments, a search...) and run `lmw har learn FILE`. |
| `LinkedIn's answer was not the contact info overlay; its format may have changed` | The contact info page came back in an unexpected format. | Look at the profile in your browser; if the website shows the contact info, the format changed and needs an update to `lmw`. |
| `request to <url> failed: <network error>` | No answer at all: no internet, DNS, firewall, proxy, timeout. | Check your connection. Corporate networks may block or intercept LinkedIn. Increase `LMW_REQUEST_TIMEOUT` on slow links. |
| `No posts found. If your feed is not empty, ...` (not an error) | The feed request worked, but no post could be read from the answer. | Check the website. If your feed has posts, record a HAR of it and compare with `lmw har inspect`. |

## Posts

| Message | What it means | Fix |
|---|---|---|
| `the post still has template placeholders to fill in, e.g. [<text>]` | Text in square brackets left from `post draft`. | Replace every `[...]` with your text. `lmw post preview` lists them all. |
| `the post is <n> characters long; the limit is 3000` | Longer than LinkedIn allows. | Shorten it. |
| `the post is empty` | No text. | Check the file or the `--text` value. |
| `give the post with --text or --text-file` | Neither flag was given. | Add one of them. |
| `use either --text or --text-file, not both` | Both flags at once. | Keep one. |
| `cannot read <file>: <details>` | The text file doesn't exist or can't be read. | Check the path. |
| `expected an idea, e.g. lmw post draft "..."` | `post draft` without an idea. | Add the idea after the command. |
| `the idea is too short to build a useful post` | Fewer than 8 characters. | Describe the idea in a few words. |
| `unsupported tone "<x>"; use one of: ...` / `unsupported length "<x>"; ...` | Unknown `--tone` or `--length`. | Use one of the listed values. |
| `Cancelled; nothing was published.` (not an error) | You typed something other than `publish`. | Run it again and type `publish` exactly. |

## Messages, network and interactions

| Message | What it means | Fix |
|---|---|---|
| `"<x>" is not a LinkedIn profile; use its address ...` | The argument isn't a profile address, name or member URN. | Paste the profile's address, e.g. `https://www.linkedin.com/in/ada-lovelace/`. |
| `"<x>" is not a LinkedIn post; use its address or URN ...` | Same, for posts. | Copy the post's link, or take its `urn` from `lmw feed --json`. |
| `"<x>" is not a LinkedIn conversation; ...` | Same, for conversations. | Use an id from `lmw messages`. |
| `no received invitation with id <id>; ...` | The invitation is not among your pending ones (answered already, withdrawn, or a sent one). | List them with `lmw invitations`. |
| `the note is <n> characters long; LinkedIn allows 300` | `connect --note` is too long. | Shorten it. |
| `unknown reaction "<x>"; use one of: ...` | `post react --type` with an unknown reaction. | Use `like`, `celebrate`, `support`, `love`, `insightful` or `funny`. |
| `... The comment may have been posted anyway: check the post before trying again` | LinkedIn answered with a server error to a comment, which it sometimes does after creating it. | Look at the post before commenting again. |
| `<file> is not a supported image; use .gif, .jpeg, ...` | The file's extension is not one LinkedIn takes for that kind of attachment. | Convert it, e.g. to PNG or JPEG for images, MP4 for video, PDF for documents. |
| `a post carries one kind of extra: images, a video, a document, a poll or a link card` | Two kinds of attachments at once. | Keep one; images can be several. |
| `a document needs a title (--title)` | `--document` without `--title`. | Add a title. |
| `a poll needs 2 to 4 options, got <n>` / `poll option "<x>" must have 1 to 30 characters` | The poll breaks LinkedIn's rules. | Adjust the options. |
| `LinkedIn refused the poll: <reason>; nothing was posted` | LinkedIn rejected the poll itself. | Fix what the reason says. |
| `a post can be scheduled from 10 minutes to 3 months ahead` | `--at` is too soon, in the past, or too far. | Pick another time. |
| `LinkedIn asked for a <type> upload of <file>, which lmw does not do; ...` | The file is so big that LinkedIn wants it uploaded in parts. | Use a smaller file. |
| `use either --to PROFILE or --conversation ID` | `messages send` needs exactly one destination. | Pick one. |
| `cannot search "<x>"; use people, companies, schools or groups` | Posts and other types can't be searched. | Use one of those; for jobs, `lmw jobs search`. |
| `Cancelled; nothing was changed on LinkedIn.` (not an error) | You typed something other than the confirmation word. | Run it again and type the word shown. |

## Keyring and files

| Message | What it means | Fix |
|---|---|---|
| `level=WARN msg="system keyring unavailable; storing the session in a private file instead"` | No keyring, typical on servers or minimal Linux installs. The session was saved in `session.json`, readable only by you. | Nothing, if that is fine for you. For the keyring on Linux, install and unlock GNOME Keyring or KWallet. |
| `cannot save the session in the system keyring: <details>` | `LMW_SESSION_STORE=keyring` is set and the keyring can't be used. | Unlock or install the keyring, or use `LMW_SESSION_STORE=auto`. |
| `cannot read the session from the system keyring: <details>` | The keyring is locked or unavailable. | Unlock it (log in to your desktop session) and retry. |
| `the session stored in <where> is corrupted; run 'lmw auth import' again` | The stored session can't be read. | Import again. |
| `<path> is not a valid identity file ... (reset it with 'lmw identity reset')` | `identity.json` was edited or damaged. | `lmw identity reset`, then re-import a HAR if you want your browser's identity. |
| `cannot create <dir>` / `cannot write <file>` | Permissions or disk space in the state directory. | Check the [state directory](configuration.md#files-on-disk), or point `LMW_STATE_DIR` somewhere writable. |

## Configuration

| Message | Fix |
|---|---|
| `LMW_<NAME> must be an integer, got "<x>"` / `... must be a number of seconds ...` | Fix or unset that variable. |
| `LMW_REQUEST_MIN_DELAY must be at least 1 second` | Use 1 or more. |
| `LMW_REQUEST_MAX_DELAY must be greater than or equal to LMW_REQUEST_MIN_DELAY` | Make the maximum at least the minimum. |
| `LMW_HOURLY_REQUEST_BUDGET cannot exceed LMW_DAILY_REQUEST_BUDGET` / `request budgets must be positive` | Adjust the budgets. |
| `LMW_REQUEST_TIMEOUT must be at least 5 seconds` / `LMW_MAX_POST_CHARS must be at least 280` | Raise the value. |
| `LMW_SESSION_STORE must be auto, keyring or file, got "<x>"` | Use one of those three. |
| `--limit must be between 1 and 50` | Use a smaller `-n`. |
| `query parameters must look like KEY=VALUE, got "<x>"` | Write `-p key=value`. |
| `only https://www.linkedin.com/voyager/api/ URLs are allowed, got "<x>"` | `api get` only accepts internal API paths, like `/me`. |

## Other situations

**Commands take several seconds.** That is the pacing: a random 2-6 second
pause between requests, sometimes longer. It is intentional; see
[Stay safe](guides/stay-safe.md). Commands that make a single request after a
long break start immediately.

**Windows says the app is from an unknown publisher.** The binary is not
code-signed. Choose *More info > Run anyway*, or build it yourself with Go.

**`lmw` is "not recognized" / "command not found".** The file is not on your
`PATH`. Run it with its path (`.\lmw.exe` or `./lmw-linux`) or see
[Getting started](getting-started.md#step-1-install-lmw).

**Pressing Ctrl+C.** `lmw` stops at once, even in the middle of a pause, and
exits with code 130. A request that was already sent may still have reached
LinkedIn.

**Something else is wrong.** Run the command with `-v` and look at the last
lines. When reporting a problem, include that output but **never** a HAR file,
`session.json` or the value of `li_at`.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Error: session, LinkedIn, network, validation or storage problem |
| `2` | Invalid usage: unknown flag, missing argument, bad value |
| `130` | Interrupted with <kbd>Ctrl</kbd>+<kbd>C</kbd> |
