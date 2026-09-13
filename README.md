# dibs

**Project-scoped port allocator.** No daemon, no background process, no config required. Ask `dibs` for a port and get one that belongs to this project, today and next week.

```
$ cd ~/Projects/shop
$ dibs postgresql
20000
$ dibs postgresql
20000
$ dibs opensearch
20001
```

Move to another project and run the same command — you get a *different* port, guaranteed not to collide with the first one.

```
$ cd ~/Projects/blog
$ dibs postgresql
20002
```

## Why

Running several projects side by side, each spinning up its own Postgres/OpenSearch/whatever for local dev, means constant `EADDRINUSE` fights over hardcoded ports. `dibs` hands out a free port per service *per project* and keeps handing out that same port every time you come back — no `docker-compose.override.yml` juggling, no manually tracking which port a project used last time.

A port is a property of the project, not of the terminal you happen to be typing in. Close your laptop, reopen it a month later, run `dibs postgresql` in the same repo: same port, so the database volume, the `.env` file and the browser bookmark you left behind all still line up.

## How it works

- **A project is a directory.** `dibs` walks up from the working directory to the nearest `.git`, and uses that repo root as the project identity. No repo, no problem: the working directory itself is the project. Symlinks are resolved and paths are matched by the directory they actually point at, so one repo reached through a symlink, a bind mount, or a differently-cased path stays one project.
- **Any subdirectory works.** Run `dibs postgresql` from the repo root, from `backend/`, from `services/api/deep/nested/dir` — same project, same port.
- **Allocations are stable, not leased.** A project keeps its ports until you release them explicitly (`dibs release`) or its directory is deleted, at which point the next `dibs` call reclaims them. A directory temporarily out of reach — an unplugged drive, a network share that's down — keeps its ports; only a directory that is genuinely gone loses them.
- **Allocation is real, not just bookkeeping.** Before handing out a port, `dibs` actually probes it with `net.Listen` — so a port used by something outside `dibs`'s own registry (left running from an old process, used by some other tool) is correctly skipped instead of double-booked.
- **State lives in one file.** `~/.local/state/dibs/registry.json` maps `(project, service) → port`, guarded by a lock file so two projects starting at the same time don't race each other.

## Install

Download the archive for your platform from the [releases page](https://github.com/pmakerhq/dibs/releases), extract the binary, and put it on your `PATH`:

```
tar -xzf dibs-vX.Y.Z-darwin-arm64.tar.gz
xattr -d com.apple.quarantine dibs   # macOS only, see below
sudo mv dibs /usr/local/bin/
```

Or build it yourself:

```
git clone git@github.com:pmakerhq/dibs.git
cd dibs
go build -o dibs .
```

### macOS: "Apple could not verify dibs is free of malware"

The released binary isn't signed or notarized, so macOS quarantines anything downloaded via a browser or `curl`. Clear the flag once, and it runs normally after that:

```
xattr -d com.apple.quarantine dibs
```

## Usage

```
dibs <service>              # get (or reuse) this project's port for <service>
dibs get <service>          # same thing, explicit form
dibs env <service...>       # print export SERVICE_PORT=<port> for one or more services
dibs list                   # list all live allocations across every project
dibs list --json            # same, as JSON
dibs release <service>      # free this project's port for <service>
dibs release --all          # free every port this project holds
dibs doctor                 # check dibs' on-disk state and config for issues
dibs --version              # print the build version
dibs completion <shell>     # generate a shell completion script
```

`<service>` is just a label — `postgresql`, `opensearch`, `redis`, whatever you're running. Every service falls back to the same generic range unless you give it its own in `config.json`.

`dibs env` derives the variable name from the service: every non-alphanumeric character becomes `_`, a leading digit gets an `_` prefix, and the whole thing is upper-cased — so `my.service` yields `MY_SERVICE_PORT` and `3scale` yields `_3SCALE_PORT`.

### Example: wiring it into a dev script

```bash
#!/usr/bin/env bash
export PGPORT=$(dibs postgresql)
docker run -p "$PGPORT:5432" postgres
```

Or with `dibs env`, for multiple services at once:

```bash
eval "$(dibs env postgresql opensearch)"
docker run -p "$POSTGRESQL_PORT:5432" postgres
docker run -p "$OPENSEARCH_PORT:9200" opensearchproject/opensearch
```

Run that script in two different repos and each gets its own containers on their own ports, with zero coordination — and re-running it tomorrow in the same repo reuses yesterday's ports.

### Shell completion

`dibs` ships with [Cobra](https://github.com/spf13/cobra)'s built-in completion generator:

```bash
source <(dibs completion zsh)    # or bash / fish / powershell
```

## Port ranges

Every service draws from one generic range (`20000–29999`) unless you give it its own in `~/.config/dibs/config.json` (respects `XDG_CONFIG_HOME`):

```json
{
  "ranges": {
    "postgresql": [15400, 15499],
    "opensearch": [19200, 19299],
    "generic": [21000, 29999]
  }
}
```

A range is only used if it describes a usable span (`1 <= low <= high <= 65535`); anything else — inverted bounds, a range including port 0 — is ignored in favour of the generic range. A malformed `config.json` is likewise ignored rather than fatal, so a bad edit never breaks port allocation. Run `dibs doctor` to see what was ignored and why.

## Diagnostics

`dibs doctor` checks the on-disk state and config, printing one line per check and exiting non-zero if anything is wrong:

```
$ dibs doctor
✓ state dir: /Users/you/.local/state/dibs
✓ registry.json: 6 entries
i 2 entries for missing projects will be cleared on next call
i range 15400-15499 is 82/100 allocated; release projects you no longer use
✓ lock file: free
✓ config: /Users/you/.config/dibs/config.json (1 range overrides)
✗ range conflict: postgresql [15400-15499] overlaps redis [15450-15460]
```

Lines marked `i` are informational and don't affect the exit code — a lock held by a concurrent `dibs` call is normal, not a fault.

## Known limitations

Ports are held per project, so two terminals in the *same* project share one port per service — that's the point. If you need two isolated instances of the same service in the same repo, ask for two different service labels (`dibs postgresql-a`, `dibs postgresql-b`).

Allocations are permanent until released. A project you abandon without deleting its directory keeps its ports reserved, so a narrow range can eventually fill up; `dibs doctor` warns once a range is 80% allocated, `dibs list` shows who holds what, and `dibs release --all` from a project frees its ports.

Running `git init` in a parent directory moves the project root up, so the enclosing repo becomes the project and gets a fresh port. The old, now unreachable entry is reclaimed automatically on the next call rather than staying reserved forever.

Allocating a port doesn't hold it. `dibs` checks the port is genuinely free (it binds it, then closes it immediately) and records it, but your service binds it some moments later. In that gap an unmanaged process could take the port. In practice the window is milliseconds and `dibs` never hands the same port to two projects; holding the socket open would require a daemon, which this tool deliberately doesn't have.

Registries written by dibs ≤ 0.1.2 were keyed by shell session; those entries carry no project path and are dropped on the first call after upgrading. Nothing to do beyond re-running `dibs` in each project.

## Development

```
go build -o dibs .    # build
go test -race ./...   # test (CI runs the race detector too)
go vet ./...          # static checks
```

See `CLAUDE.md` for architecture details and conventions. Cutting a release is handled by the `/release` Claude Code skill in this repo, which tags a commit and lets `.github/workflows/release.yml` build and publish it.
