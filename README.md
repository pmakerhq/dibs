# dibs

**Session-scoped port allocator.** No daemon, no background process, no config required. Ask `dibs` for a port, get one that's yours until your shell closes.

```
$ dibs postgresql
15400
$ dibs postgresql
15400
$ dibs opensearch
19200
```

Open a second terminal and run the same command — you get a *different* port, guaranteed not to collide with the first shell's.

## Why

Running several projects side by side, each spinning up its own Postgres/OpenSearch/whatever for local dev, means constant `EADDRINUSE` fights over hardcoded ports. `dibs` hands out a free port per service, remembers it for as long as your shell lives, and cleans up after itself once the shell exits — no `docker-compose.override.yml` juggling, no manually tracking which port you used last time.

## How it works

- **A shell is a session.** `dibs` identifies the calling shell by its parent PID plus that PID's exact start time, hashed into a session key. Same shell, same key, every time — call `dibs postgresql` five times in a row and you get the same port back. A new terminal tab has a different PID/start-time pair, so it gets its own allocation instead of colliding.
- **Ports are leased, not owned forever.** Every call does a quick garbage-collection pass: any session whose shell process is no longer alive gets its ports freed automatically. There's nothing to clean up by hand and nothing running in the background between calls.
- **Allocation is real, not just bookkeeping.** Before handing out a port, `dibs` actually probes it with `net.Listen` — so a port used by something outside `dibs`'s own registry (left running from an old process, used by some other tool) is correctly skipped instead of double-booked.
- **State lives in one file.** `~/.local/state/dibs/registry.json` maps `(session, service) → port`, guarded by a lock file so concurrent shells calling `dibs` at the same time don't race each other.

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
dibs <service>            # get (or reuse) this session's port for <service>
dibs get <service>        # same thing, explicit form
dibs list                 # list all live allocations across every session
dibs release <service>    # free this session's port for <service> early
dibs --version            # print the build version
```

`<service>` is just a label — `postgresql`, `opensearch`, `redis`, whatever you're running. Known services get sensible built-in ranges; anything else falls back to a generic range.

### Example: wiring it into a dev script

```bash
#!/usr/bin/env bash
export PGPORT=$(dibs postgresql)
docker run -p "$PGPORT:5432" postgres
```

Run that script from two different terminals and each gets its own Postgres container on its own port, with zero coordination.

## Built-in port ranges

| Service      | Range         |
|--------------|---------------|
| `postgresql` | 15400–15499   |
| `opensearch` | 19200–19299   |
| *(anything else)* | 20000–29999 |

Override any of these — or the generic fallback — in `~/.config/dibs/config.json` (respects `XDG_CONFIG_HOME`):

```json
{
  "ranges": {
    "postgresql": [16000, 16099],
    "generic": [21000, 29999]
  }
}
```

## Known limitation

`dibs` must be invoked directly from the interactive shell, not through an intermediate forked subshell (some `bash -c` invocations, depending on whether bash tail-exec-optimizes the call away). A forked subshell has its own PID, so calls from it may not resolve to the same session as its parent shell.

## Development

```
go build -o dibs .   # build
go test ./...        # test
go vet ./...          # static checks
```

See `CLAUDE.md` for architecture details and conventions. Cutting a release is handled by the `/release` Claude Code skill in this repo, which tags a commit and lets `.github/workflows/release.yml` build and publish it.
