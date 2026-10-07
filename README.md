# octopus-autojoin

A small Go CLI for Octopus Energy account tasks: joining eligible Power Down Saving Sessions, using available Wheel of Fortune spins, and viewing Wheel of Fortune history.

The account-changing commands default to a dry run. All commands share API-key authentication and can run unattended using systemd timers.

## Prerequisites

`octopus-autojoin` requires these environment variables:

- `OCTOPUS_API_KEY`
- `OCTOPUS_ACCOUNT_NUMBER`

For local development, copy `.env.example` to `.env` and fill in your values. The application optionally loads `./.env`; it is also fine to set the variables directly in your shell. Values already present in the process environment take precedence over values in `.env`.

In production, systemd can provide the same variables with an `EnvironmentFile=/etc/octopus-autojoin/environment` service setting. The path is part of the service deployment configuration; the application reads only environment variables.

Keep credentials private. Never commit `.env` or any file containing real secrets.

## Commands

Both commands are one-shot jobs and default to a **dry run**. Add `--execute` only when you want them to make changes to your account.

```sh
# Existing invocation still checks/joins Power Down Saving Sessions.
octopus-autojoin
octopus-autojoin --execute

# Equivalent explicit command.
octopus-autojoin saving-sessions --execute

# Check available electricity and gas spins without using them.
octopus-autojoin wheel-of-fortune

# Use available spins for both fuels and log each result.
octopus-autojoin wheel-of-fortune --execute

# Show all available Wheel of Fortune history.
octopus-autojoin wheel-of-fortune-history

# Filter history by date and fuel.
octopus-autojoin wheel-of-fortune-history --from 2026-01-01 --to 2026-01-31 --fuel electricity
```

Put the command before its flags. `--help` and `--version` work without credentials. The binary name, module path, environment variables and default Saving Sessions behaviour are unchanged. Renaming the project can be handled separately.

`wheel-of-fortune-history` is read-only and never needs `--execute`. Its optional `--from` and `--to` flags use `YYYY-MM-DD`; `--fuel` accepts `electricity` or `gas` and is omitted when not set. It retrieves every matching page, then sorts the results newest first. The table shows timestamp, prize and prize type. It prefers the API's prize display text and uses the raw value only when display text is unavailable, without assigning a currency or points unit. Missing fields and empty history are shown clearly. If any page fails or is incomplete, the command exits with an error and does not print a partial table.

### Wheel of Fortune behaviour

The `wheel-of-fortune` command uses the same API key and account number as Saving Sessions. It queries the backend for the available allowance rather than assuming a fixed number of monthly spins. With no spins available, it exits successfully.

Each spin is followed by a fresh allowance check. The command stops with an error if the count does not decrease, a response is incomplete, or an API request fails. It never attempts more spins per fuel than were available at the start of the run. Spin mutations are not automatically retried: a timeout could mean the server used the spin but the response was lost. A later invocation checks the remaining allowance afresh. Successful spins and their results remain in the logs if a later request fails. A successful spin can have no prize value; this is logged rather than treated as a failed spin.

`prize_value` is the raw API value, without an assumed points/currency unit. The command does not redeem Octopoints into account credit.

Requests use `wheelOfFortuneSpinsAllowed` and `spinWheelOfFortune` at `https://api.backend.octopus.energy/v1/graphql/`, following the operations used by [Home Assistant Octopus Energy](https://github.com/BottlecapDave/HomeAssistant-OctopusEnergy/blob/develop/custom_components/octopus_energy/api_client/__init__.py). These operations are covered by mocked API tests; a dry run against your own account should precede enabling execution.

### Scheduling with systemd

On Linux systems using systemd, each command can run automatically using a one-shot service and a timer:

- Saving Sessions: every 20 minutes.
- Wheel of Fortune: daily, using whatever spins are available.

Both services share the same binary and credentials.

#### Install the binary

Download or build the binary for your Linux machine’s architecture, then install it from the directory containing it:

```sh
sudo install -m 0755 octopus-autojoin /usr/local/bin/octopus-autojoin
```

If the downloaded binary has a platform suffix, use that filename as the source.

Create a dedicated service account, unless it already exists:

```sh
sudo useradd --system --user-group \
  --home-dir /nonexistent \
  --shell /usr/sbin/nologin \
  octopus-autojoin
```

#### Configure credentials

Create a configuration directory and a credentials file readable only by root:

```sh
sudo install -d -m 0755 /etc/octopus-autojoin
sudo touch /etc/octopus-autojoin/environment
sudo chmod 0600 /etc/octopus-autojoin/environment
sudo chown root:root /etc/octopus-autojoin/environment
sudoedit /etc/octopus-autojoin/environment
```

Add your API key and account number:

```ini
OCTOPUS_API_KEY=your-api-key
OCTOPUS_ACCOUNT_NUMBER=A-YOURACCOUNT
```

Systemd reads this file before starting the process as the service account. The application does not need direct access to the file.

#### Create the Saving Sessions service and timer

Create `/etc/systemd/system/octopus-autojoin.service`:

```ini
[Unit]
Description=Join eligible Octopus Saving Sessions
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
User=octopus-autojoin
Group=octopus-autojoin
EnvironmentFile=/etc/octopus-autojoin/environment
ExecStart=/usr/local/bin/octopus-autojoin saving-sessions --execute
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
```

Create `/etc/systemd/system/octopus-autojoin.timer`:

```ini
[Unit]
Description=Check Octopus Saving Sessions every 20 minutes

[Timer]
OnCalendar=*:00,20,40
Persistent=true
Unit=octopus-autojoin.service

[Install]
WantedBy=timers.target
```

#### Create the Wheel of Fortune service and timer

Create `/etc/systemd/system/octopus-wheel-of-fortune.service`:

```ini
[Unit]
Description=Use available Octopus Wheel of Fortune spins
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
User=octopus-autojoin
Group=octopus-autojoin
EnvironmentFile=/etc/octopus-autojoin/environment
ExecStart=/usr/local/bin/octopus-autojoin wheel-of-fortune --execute
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
```

Create `/etc/systemd/system/octopus-wheel-of-fortune.timer`:

```ini
[Unit]
Description=Check Octopus Wheel of Fortune daily

[Timer]
OnCalendar=*-*-* 09:00:00 Europe/London
RandomizedDelaySec=15m
Persistent=true
Unit=octopus-wheel-of-fortune.service

[Install]
WantedBy=timers.target
```

The wheel check runs between 09:00 and 09:15 UK time. Running it daily picks up unused spins; it does not increase the monthly allowance.

`Persistent=true` allows a missed scheduled run to happen when the timer next starts, for example after the machine has been switched off.

#### Check configuration and enable scheduling

First, run a dry run using the installed binary and credentials:

```sh
sudo systemd-run --wait --pipe --collect \
  --property=User=octopus-autojoin \
  --property=EnvironmentFile=/etc/octopus-autojoin/environment \
  /usr/local/bin/octopus-autojoin saving-sessions

sudo systemd-run --wait --pipe --collect \
  --property=User=octopus-autojoin \
  --property=EnvironmentFile=/etc/octopus-autojoin/environment \
  /usr/local/bin/octopus-autojoin wheel-of-fortune
```

These commands omit `--execute`, so they check availability without joining sessions or using spins.

Once both checks succeed, load the unit files and enable the timers:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now octopus-autojoin.timer
sudo systemctl enable --now octopus-wheel-of-fortune.timer
```

Enable only the timers for the features you want. If replacing an older schedule, disable any duplicate timers or cron jobs first.

#### Monitor scheduled runs

Show the next scheduled runs:

```sh
systemctl list-timers 'octopus-*'
```

Read the results and any errors:

```sh
sudo journalctl -u octopus-autojoin.service
sudo journalctl -u octopus-wheel-of-fortune.service
```

To execute a service immediately:

```sh
sudo systemctl start octopus-autojoin.service
sudo systemctl start octopus-wheel-of-fortune.service
```

These services include `--execute`, so starting them performs account actions.

Systemd prevents overlapping runs of the same service. Avoid running the wheel command directly while its service is active, because separate CLI processes do not share a lock.

## Development

```sh
go test ./...
go vet ./...
go build -o octopus-autojoin .
```

The code is split into:

- `main.go`: command selection and process setup.
- `internal/config`: shared environment and `.env` loading.
- `internal/octopus`: shared Kraken authentication and GraphQL HTTP client.
- `internal/savingsessions`: Saving Sessions queries, filtering and joining.
- `internal/wheeloffortune`: spin availability, execution and result logging.

Feature packages share the authenticated client and own their API operations.
