# sweep

[![CI](https://github.com/kanywst/sweep/actions/workflows/ci.yml/badge.svg)](https://github.com/kanywst/sweep/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kanywst/sweep)](https://github.com/kanywst/sweep/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/kanywst/sweep.svg)](https://pkg.go.dev/github.com/kanywst/sweep)

Find what you left running and stop it.

<img src="demo.gif" alt="sweep stopping forgotten dev servers" width="800">

Dev servers, `python -m http.server`, `npx serve`: they pile up for weeks and hold ports. `sweep` lists every process with a listening port, grouped by the directory it was started from, oldest first, and stops the ones you pick, along with the `npm` / `pnpm` / `sh -c` launchers above them.

## Install

```sh
# Homebrew (macOS)
brew install kanywst/tap/sweep

# Go
go install github.com/kanywst/sweep/cmd/sweep@latest
```

Linux `.deb` / `.rpm` packages and archives are on the [releases page](https://github.com/kanywst/sweep/releases). Release checksums are signed with cosign, and archives carry build provenance (`gh attestation verify <archive> -R kanywst/sweep`).

## Use

```sh
sweep            # interactive view
sweep ports      # plain list
sweep ports --json
sweep ports --all  # include system services (working directory "/")
```

In the interactive view:

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k` | move |
| `space` | select a process, or a whole directory on its heading |
| `a` | select the directory under the cursor |
| `x` | stop the selection (or the process under the cursor) |
| `r` | refresh |
| `?` | all keys |
| `q` | quit |

Stopping sends `SIGTERM`, waits 3 seconds, then sends `SIGKILL` to anything still running. Right before each signal sweep checks that the pid still belongs to the process it scanned (same start time), so a pid reused while the view sat open is skipped, not signalled. A launcher such as `pnpm dev` or `sh -c` is stopped along with its server only when it has no other child, so a `pnpm -r` running several servers, or a tmux server, is never taken down. Nothing is stopped without a confirmation, and `enter` never confirms one: press `y`. Stopped rows stay on screen, struck through, until you refresh, and the header keeps a running count of what you swept.

`NO_COLOR` is respected. Set `SWEEP_NO_ANIM=1` to turn off the flash on the swept counter.

## Requirements

macOS or Linux with `lsof` and `ps` (the `lsof` and `procps` packages on Linux; the `.deb` and `.rpm` depend on them). sweep runs them from `/usr/sbin`, `/usr/bin` or `/bin` directly, never through `PATH`.

## Development

```sh
make test       # go test -race ./...
make lint       # golangci-lint run
make vulncheck  # go tool govulncheck ./...
make fix        # go fix + golangci-lint --fix
```

## License

Apache-2.0
