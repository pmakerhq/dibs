## Project

`dibs` — session-scoped port allocator. Run `dibs <service>` from a shell to get a free port for that service (e.g. `postgresql`, `opensearch`). Repeated calls from the same shell return the same port. A different shell (new terminal/tab/pane) gets a different, non-colliding port. Ports are released automatically once the owning shell exits (lazy GC on next call) — no daemon, no background process.

Go, single static binary, no daemon. Lives at `~/Projects/pmaker/dibs`, independent of any other project (usable from oddscore-platform, android, or anywhere else).

## Commands

- `go build -o dibs .` — build the binary
- `go test ./...` — run tests
- `go vet ./...` — static checks
- `./dibs <service>` — get/reuse this session's port (e.g. `./dibs postgresql`)
- `./dibs get <service>` — same, explicit form
- `./dibs env <service...>` — print `export SERVICE_PORT=<port>` for one or more services
- `./dibs list` / `./dibs list --json` — list live allocations (also runs GC)
- `./dibs release <service>` / `./dibs release --all` — free this session's port(s) early
- `./dibs doctor` — check on-disk state (registry, lock, range config) for issues

## Architecture

**Session identity** (`session.go`): a shell is identified by its PID (`$PPID` as seen by `dibs`, i.e. the parent process that invoked it) plus that PID's exact start time (`gopsutil` `process.CreateTime()`), hashed into a short session key. Same shell → same key on every call. A new shell has a different PID+start time → different key. If the OS recycles a PID after the original shell exited, the start time won't match, so it's correctly treated as a new session, not a collision.

Known limitation: `dibs` must be called directly from the interactive shell, not through an intermediate forked subshell (e.g. some `bash -c` invocations, depending on whether bash tail-exec-optimizes the call away). A forked subshell has its own PID, so calls from it may not resolve to the same session as the parent shell.

**Registry** (`registry.go`): `~/.local/state/dibs/registry.json`, one entry per `(session_key, service)` → port. Writes are guarded by an exclusive lock (`~/.local/state/dibs/.lock`, via `gofrs/flock`) so two shells calling `dibs` at once don't race. Every call does a lazy GC pass first: any entry whose owning PID is dead or whose start time no longer matches is dropped and its port freed.

**Port ranges** (`ranges.go`): built-in per-service ranges (`postgresql: 15400-15499`, `opensearch: 19200-19299`), unknown services fall back to a generic range (`20000-29999`). Allocation picks the first port in range that's neither already held by a live session nor actually bound on the host (`net.Listen` probe) — so a port used by something outside `dibs`'s own registry is still correctly skipped.

Ranges are overridable via `~/.config/dibs/config.json` (`config.go`, respects `XDG_CONFIG_HOME`):
```json
{
  "ranges": {
    "postgresql": [16000, 16099],
    "generic": [21000, 29999]
  }
}
```
`generic` overrides the fallback range; any other key overrides that service's range, built-in or not.

## Style

Same conventions as the user's other projects: no comments except where a non-obvious constraint or workaround needs explaining, tests alongside implementation, terse commits. Commit messages always in English, regardless of the language used elsewhere in conversation.

## Guidelines

Behavioral guidelines to reduce common LLM coding mistakes (adapted from [andrej-karpathy-skills](https://github.com/forrestchang/andrej-karpathy-skills)). Bias toward caution over speed; use judgment on trivial tasks.

1. **Think before coding.** Don't assume, don't hide confusion. State assumptions explicitly; if multiple interpretations exist, present them instead of picking silently; if something is unclear, stop and ask.
2. **Simplicity first.** Minimum code that solves the problem — no speculative features, no unrequested abstractions or configurability, no error handling for impossible scenarios.
3. **Surgical changes.** Touch only what the task requires: don't "improve" adjacent code or refactor things that aren't broken, match existing style, only remove dead code your own change orphaned. Every changed line should trace to the request.
4. **Goal-driven execution.** Turn tasks into verifiable goals (e.g. "fix the bug" → write a failing test, then make it pass) and loop until verified before claiming done.
