## Project

`dibs` — project-scoped port allocator. Run `dibs <service>` from inside a project to get a free port for that service (e.g. `postgresql`, `opensearch`). Repeated calls from anywhere in that project — any subdirectory, any terminal, any day — return the same port. A different project gets a different, non-colliding port. A project's ports are freed explicitly (`dibs release`) or automatically once its directory no longer exists (lazy GC on next call) — no daemon, no background process.

Go, single static binary, no daemon. Lives at `~/Projects/pmaker/dibs`, independent of any other project (usable from oddscore-platform, android, or anywhere else).

## Commands

- `go build -o dibs .` — build the binary
- `go test ./...` — run tests
- `go vet ./...` — static checks
- `./dibs <service>` — get/reuse this project's port (e.g. `./dibs postgresql`)
- `./dibs get <service>` — same, explicit form
- `./dibs env <service...>` — print `export SERVICE_PORT=<port>` for one or more services
- `./dibs list` / `./dibs list --json` — list live allocations (also runs GC)
- `./dibs release <service>` / `./dibs release --all` — free this project's port(s)
- `./dibs doctor` — check on-disk state (registry, lock, range config) for issues

## Architecture

**Project identity** (`project.go`): a project is a directory. `projectRoot` walks up from the working directory to the nearest ancestor containing a `.git` entry and uses that path as the identity; with no `.git` anywhere above, the working directory itself is the project. Paths go through `filepath.EvalSymlinks`, and `sameProject` falls back to `os.SameFile` so a repo reached through a differently-cased path (case-insensitive APFS) or a bind mount still resolves to one allocation. Nothing about the calling process matters — no PID, no start time, no per-OS code.

`projectAlive` decides whether an entry survives GC: gone if the directory returns `fs.ErrNotExist`, or if the path is no longer its own root (a `git init` in a parent makes the old entry unaddressable, so it must be reclaimed rather than holding a port forever). Any other stat error — unplugged drive, unreachable network mount — keeps the entry, since a detached disk must not cost a project its stable port.

**Registry** (`registry.go`): `~/.local/state/dibs/registry.json`, one entry per `(project, service)` → port. Writes are guarded by an exclusive lock (`~/.local/state/dibs/.lock`, via `gofrs/flock`) so two projects calling `dibs` at once don't race. Every call does a lazy GC pass first: any entry `projectAlive` rejects is dropped and its port freed. Entries from the pre-0.2 session-keyed format carry no project path, so the same pass drops them — that's the whole migration story.

**Port ranges** (`ranges.go`): built-in per-service ranges (`postgresql: 15400-15499`, `opensearch: 19200-19299`), unknown services fall back to a generic range (`20000-29999`). Allocation picks the first port in range that's neither already held by another project nor actually bound on the host (`net.Listen` probe) — so a port used by something outside `dibs`'s own registry is still correctly skipped.

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
