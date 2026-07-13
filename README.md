# Keep Me Alive

[![CI](https://github.com/LycheeOrg/Keep-Me-Alive/actions/workflows/ci.yml/badge.svg)](https://github.com/LycheeOrg/Keep-Me-Alive/actions/workflows/ci.yml)
[![OpenSSF Scorecard][ossf-shield]](https://securityscorecards.dev/viewer/?uri=github.com/LycheeOrg/Keep-Me-Alive)


A small Go binary that periodically checks a configured list of websites — some run locally via `docker compose`, some remote — restarts local ones that go down, and notifies a Signal group via [signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api) when a site goes down or recovers.

## Features

- Local sites (checked over HTTP, restarted with a configurable shell command) and remote sites (checked only).
- Signal notifications via signal-cli-rest-api, behind HTTP Basic Auth.
- Notifications fire only on state *transitions* (healthy→down, down→healthy) — no repeat spam while an outage continues. Local-site restarts are still retried every cycle while a site stays down.
- Check history persisted to a local SQLite database, viewable live via a status TUI.
- Four run modes: one-time check, daemon, verbose daemon (debug), and a live status view.

## Installation

Requires Go 1.25+ (or `GOTOOLCHAIN=auto`, the default, to fetch it automatically).

```sh
git clone https://github.com/LycheeOrg/Keep-Me-Alive.git
cd Keep-Me-Alive
go build -o keep-me-alive .
```

Prebuilt binaries for Linux and macOS (amd64/arm64) are attached to each [GitHub release](https://github.com/LycheeOrg/Keep-Me-Alive/releases).

## Configuration

Configuration lives in a YAML file (default `config.yaml`, override with `-c`/`--config`).

| Field | Type | Description |
|---|---|---|
| `check_interval` | duration | Daemon-mode poll interval (e.g. `60s`) |
| `restart_recheck_delay` | duration | Wait after restarting a local site before rechecking it |
| `http_timeout` | duration | Per-request timeout for site checks and Signal calls |
| `state_db_path` | string | SQLite file for check history, read by `-s` |
| `history_size` | int | Max recent checks retained per site (ring buffer) |
| `signal.base_url` | string | URL of the signal-cli-rest-api instance |
| `signal.username` / `signal.password` | string | HTTP Basic Auth credentials for the Signal API |
| `signal.sender_number` | string | Registered sender number |
| `signal.recipients` | []string | Recipient numbers or `group.<id>` group IDs |
| `sites[].name` | string | Unique site name |
| `sites[].type` | `local` \| `remote` | Whether the site is restarted locally |
| `sites[].url` | string | URL checked with a GET request; 2xx = up |
| `sites[].restart_command` | string | (local only) Shell command run via `sh -c` to restart the site |
| `sites[].working_dir` | string | (local only) Directory the restart command runs in |

Example (see [`config.yaml`](config.yaml)):

```yaml
check_interval: 60s
restart_recheck_delay: 15s
http_timeout: 10s
state_db_path: "keep-me-alive.db"
history_size: 50

signal:
  base_url: "http://signal-host:8080"
  username: "proxyuser"
  password: "proxypass"
  sender_number: "+15551234567"
  recipients:
    - "group.abcdef123456=="

sites:
  - name: "lychee"
    type: local
    url: "http://localhost:8081/api/health"
    restart_command: "docker compose restart lychee"
    working_dir: "/opt/lychee"

  - name: "example-remote"
    type: remote
    url: "https://example.com/"
```

## Usage

```sh
keep-me-alive -o             # one-time check, prints a results table, exits
keep-me-alive -d             # daemon: runs forever at check_interval, quiet logging
keep-me-alive                # same as -d, but with verbose/debug logging
keep-me-alive -s             # live status TUI, one line per host
keep-me-alive -c other.yaml  # use a different config file (any mode)
```

`-o`, `-d`, and `-s` are mutually exclusive.

### Down-handling behavior

- **Remote site down** → a Signal notification is sent immediately (once, on the down transition).
- **Local site down** → the configured `restart_command` is run in `working_dir`, then after `restart_recheck_delay` the site is rechecked. If it's still down, a Signal notification is sent. If it recovered, nothing is sent (just logged). While a local site remains down across daemon cycles, restarts keep being retried every cycle, but the notification only fires once per outage.
- **Recovery** → a Signal notification is sent once when a previously-down site comes back up.

One-time mode (`-o`) has no state carried over between invocations — each run is independent.

### Status view (`-s`)

Reads `state_db_path` and shows one line per configured site: current status, last-checked time, latency, and a sparkline of recent checks. It doesn't perform any checks itself, so it works alongside a running daemon or after one has stopped, live-refreshing every 2 seconds. Press `q` to quit.

## Development

```sh
make build       # go build -o keep-me-alive .
make vet         # go vet ./...
make fmt-check   # gofmt -l . (fails if anything is unformatted)
make test        # go test ./...
make test-race   # go test -race ./...
make all         # build + vet + fmt-check + test
```

Tests are self-contained (using `httptest` servers and temporary SQLite files) and don't require Docker or a real Signal server.

[ossf-shield]: https://api.securityscorecards.dev/projects/github.com/LycheeOrg/Keep-Me-Alive/badge

