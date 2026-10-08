# octo-cli


[![Release](https://img.shields.io/github/v/release/rogerhutchings/octo-cli?style=flat-square&logo=github&label=release)](https://github.com/rogerhutchings/octo-cli/releases)
[![Release checks](https://img.shields.io/github/actions/workflow/status/rogerhutchings/octo-cli/release.yml?style=flat-square&label=release%20checks)](https://github.com/rogerhutchings/octo-cli/actions/workflows/release.yml)
[![Go](https://img.shields.io/badge/Go-1.27.1-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)


`octo-cli` is a Go command-line application for a small set of Octopus Energy account tasks. It can list and join eligible Saving Sessions, check Wheel of Fortune allowances, use spins, inspect spin history and report Scratchcard status. It is an independent project and is not affiliated with or endorsed by Octopus Energy.

## Features

- **Saving Sessions:** list upcoming sessions with eligibility reasons and join eligible sessions.
- **Wheel of Fortune:** check electricity and gas allowances, use available spins and inspect spin history.
- **Scratchcard:** view active session and card status exposed by Octopus's authenticated backend.
- **Dry-run defaults:** account-changing commands only make changes when you pass `--execute`.

Scratchcard status is read-only. Scratchcard participation is not implemented.

## Installation

The [latest release](https://github.com/rogerhutchings/octo-cli/releases/latest) provides a Linux amd64 binary and a SHA-256 checksum. Download and verify the binary, then install it in a directory on your `PATH`:

```sh
curl -fL https://github.com/rogerhutchings/octo-cli/releases/latest/download/octo-cli-linux-amd64 \
  -o /tmp/octo-cli-linux-amd64
curl -fL https://github.com/rogerhutchings/octo-cli/releases/latest/download/octo-cli-linux-amd64.sha256 \
  -o /tmp/octo-cli-linux-amd64.sha256
(cd /tmp && sha256sum -c octo-cli-linux-amd64.sha256)
sudo install -m 0755 /tmp/octo-cli-linux-amd64 /usr/local/bin/octo-cli
rm /tmp/octo-cli-linux-amd64 /tmp/octo-cli-linux-amd64.sha256
```

To build from source, install the Go version declared in [`go.mod`](go.mod), then run `go build -o octo-cli .` from the repository. The release workflow currently publishes Linux amd64 only.

## Configuration

Commands that contact Octopus require these credentials:

| Variable | Purpose |
| --- | --- |
| `OCTOPUS_API_KEY` | Authenticates with Octopus Energy to obtain an API token. |
| `OCTOPUS_ACCOUNT_NUMBER` | Selects the account used by the command. |

For local use, copy the example file and add your own values:

```sh
cp .env.example .env
```

Edit `.env` without sharing or committing it. Run `octo-cli` from the repository directory so it can load `./.env`. Values already set in the process environment take precedence over values in `.env`. You can also export the variables directly instead of using a file. Help and version commands do not need credentials.

## Usage

```text
octo-cli saving-sessions join [--execute]
octo-cli saving-sessions list
octo-cli wheel spin [--execute] [--max-spins N]
octo-cli wheel status
octo-cli wheel history [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--fuel electricity|gas]
octo-cli scratchcard status
```

For example:

```sh
# Review upcoming Saving Sessions and their eligibility
octo-cli saving-sessions list

# Preview joining eligible sessions, then join them explicitly
octo-cli saving-sessions join
octo-cli saving-sessions join --execute

# Check allowances, preview one spin, then use at most one spin
octo-cli wheel status
octo-cli wheel spin --max-spins 1
octo-cli wheel spin --execute --max-spins 1

# Review January electricity spin history
octo-cli wheel history --from 2026-01-01 --to 2026-01-31 --fuel electricity

# Check the active Scratchcard session and status
octo-cli scratchcard status
```

`join` and `spin` are dry runs unless `--execute` is supplied. `saving-sessions list`, `wheel status`, `wheel history` and `scratchcard status` are read-only. Scratchcard status reports information exposed by Octopus's authenticated backend; a missing Scratchcard does not confirm that a play is available. The Saving Sessions commands consider upcoming `TURN_DOWN` events for the account's region that have not already been joined; `list` also shows ineligible upcoming events and their reasons.

`--max-spins N` sets a positive limit across electricity and gas combined for one invocation. Electricity spins are used first. If the option is omitted, `wheel spin` can use the available allowance for both fuels. `wheel status` shows the current allowance, and `wheel history` can filter records by date and fuel.

Use `octo-cli --help` or `octo-cli <group> --help` for command help. History dates use `YYYY-MM-DD`.

## Automation

`octo-cli` runs once and can be scheduled by an external service such as systemd. For unattended setup, schedules, credentials and operational commands, see [Automation and scheduling](AUTOMATION.md).

## Development

Build and validate changes with:

```sh
go build -o octo-cli .
go test ./...
go vet ./...
```

See [AGENTS.md](AGENTS.md) for development and repository guidance.
