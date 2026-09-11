## Project

`dibs` — session-scoped port allocator. Run `dibs <service>` from a shell to get a free port for that service (e.g. `postgresql`, `opensearch`). Repeated calls from the same shell return the same port. A different shell (new terminal/tab/pane) gets a different, non-colliding port. Ports are released automatically once the owning shell exits (lazy GC on next call) — no daemon, no background process.

Go, single static binary, no daemon. Lives at `~/Projects/pmaker/dibs`, independent of any other project (usable from oddscore-platform, android, or anywhere else).

## Commands

- `go build -o dibs .` — build the binary
- `go test ./...` — run tests
- `go vet ./...` — static checks
- `./dibs <service>` — get/reuse this session's port (e.g. `./dibs postgresql`)
- `./dibs get <service>` — same, explicit form
- `./dibs list` — list live allocations (also runs GC)
- `./dibs release <service>` — free this session's port for `<service>` early

## Architecture

**Session identity** (`session.go`): a shell is identified by its PID (`$PPID` as seen by `dibs`, i.e. the parent process that invoked it) plus that PID's exact start time (`gopsutil` `process.CreateTime()`), hashed into a short session key. Same shell → same key on every call. A new shell has a different PID+start time → different key. If the OS recycles a PID after the original shell exited, the start time won't match, so it's correctly treated as a new session, not a collision.

Known limitation: `dibs` must be called directly from the interactive shell, not through an intermediate forked subshell (e.g. some `bash -c` invocations, depending on whether bash tail-exec-optimizes the call away). A forked subshell has its own PID, so calls from it may not resolve to the same session as the parent shell.

**Registry** (`registry.go`): `~/.local/state/dibs/registry.json`, one entry per `(session_key, service)` → port. Writes are guarded by an exclusive lock (`~/.local/state/dibs/.lock`, via `gofrs/flock`) so two shells calling `dibs` at once don't race. Every call does a lazy GC pass first: any entry whose owning PID is dead or whose start time no longer matches is dropped and its port freed.

**Port ranges** (`ranges.go`): built-in per-service ranges (`postgresql: 15400-15499`, `opensearch: 19200-19299`), unknown services fall back to a generic range (`20000-29999`). Allocation picks the first port in range that's neither already held by a live session nor actually bound on the host (`net.Listen` probe) — so a port used by something outside `dibs`'s own registry is still correctly skipped.

## Style

Same conventions as the user's other projects: no comments except where a non-obvious constraint or workaround needs explaining, tests alongside implementation, terse commits.
