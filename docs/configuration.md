# Configuration

`lmw` needs no configuration. Everything below is optional.

## Environment variables

| Variable | Default | Allowed | Meaning |
|---|---|---|---|
| `LMW_REQUEST_MIN_DELAY` | `2` | ≥ 1 | Shortest pause between requests, in seconds (decimals allowed) |
| `LMW_REQUEST_MAX_DELAY` | `6` | ≥ min | Longest regular pause, in seconds. One pause in ten adds between 1× and 3× this value |
| `LMW_HOURLY_REQUEST_BUDGET` | `60` | 1 to daily | Maximum requests in any 60 minutes |
| `LMW_DAILY_REQUEST_BUDGET` | `300` | ≥ 1 | Maximum requests in any 24 hours |
| `LMW_REQUEST_TIMEOUT` | `30` | ≥ 5 | Seconds to wait for LinkedIn's answer |
| `LMW_MAX_POST_CHARS` | `3000` | ≥ 280 | Longest post `preview` and `publish` accept |
| `LMW_SESSION_STORE` | `auto` | `auto`, `keyring`, `file` | Where the session is stored; `auto` uses the keyring and falls back to a file |
| `LMW_STATE_DIR` | see below | a directory | Where `lmw` keeps its files |

Invalid values stop `lmw` with a message and exit code 2 before anything is sent.

**Setting them**

```bash
# Linux / macOS: for one command
LMW_REQUEST_MIN_DELAY=4 lmw feed -n 30

# Linux / macOS: for the whole terminal session
export LMW_REQUEST_MIN_DELAY=4
```

```powershell
# Windows PowerShell: for the current window
$env:LMW_REQUEST_MIN_DELAY = "4"
lmw feed -n 30
```

## Files on disk

`lmw` keeps its files in one state directory:

| System | Default location |
|---|---|
| Linux | `$XDG_STATE_HOME/linkedin-mecha-warrior`, usually `~/.local/state/linkedin-mecha-warrior` |
| Windows | `%LOCALAPPDATA%\LinkedinMechaWarrior` |
| macOS | `~/Library/Application Support/LinkedinMechaWarrior` |

The directory is created with permissions for your user only.

| File | Contents | Sensitive | Safe to delete? |
|---|---|:---:|---|
| `request-log.json` | Timestamps of the requests of the last 24 h, and any active cooldown | no | Yes, but it resets the budgets and cooldowns, which defeats their purpose |
| `identity.json` | The browser identity imported from a HAR (no cookies) | a little | Yes: `lmw identity reset` does it |
| `graphql-queries.json` | GraphQL query versions learned from HAR files with `lmw har learn` | no | Yes: `lmw` goes back to its built-in versions |
| `session.json` | The session cookies, **only** when no keyring is available | **yes** | Yes: `lmw auth logout` does it |

When a keyring is available, the session is stored there instead, under the
service name `linkedin-mecha-warrior` and the account `session`. You can see
it in Windows Credential Manager, macOS Keychain Access, or Seahorse on Linux.

## Global options

These work with every command, before or after its name:

| Option | Effect |
|---|---|
| `--json` | Machine-readable output for commands that show data |
| `-v`, `--verbose` | Debug logs on stderr: each request's URL, status and size, pauses, cookie updates |
| `-h`, `--help` | Help for the command |
| `--version` | The version (only on `lmw` itself) |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Error (session, LinkedIn, network, validation, storage) |
| `2` | Invalid usage or configuration |
| `130` | Interrupted with <kbd>Ctrl</kbd>+<kbd>C</kbd> |

Output goes to stdout; errors, warnings, prompts and debug logs go to stderr.
With `--json`, stdout carries only the JSON: messages for people, such as the
post that `post publish` shows before asking for confirmation, go to stderr
too. That keeps `lmw feed --json > feed.json` clean even when something is
logged.
