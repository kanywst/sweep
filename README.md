# sweep

Find what you left running and stop it.

Dev servers, `python -m http.server`, `npx serve`: they pile up for weeks and hold ports. `sweep` lists every process with a listening port, grouped by the directory it was started from, oldest first, and stops the ones you pick, along with the `npm` / `pnpm` / `sh -c` launchers above them.

## Install

```sh
go install github.com/kanywst/sweep/cmd/sweep@latest
```

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

Stopping sends `SIGTERM`, waits 3 seconds, then sends `SIGKILL` to anything still running. Nothing is stopped without a confirmation.

## Requirements

macOS or Linux with `lsof` and `ps`.

## Development

```sh
make test       # go test -race ./...
make lint       # golangci-lint run
make vulncheck  # go tool govulncheck ./...
make fix        # go fix + golangci-lint --fix
```

## License

Apache-2.0
