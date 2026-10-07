# octo-cli

`octo-cli` is a small Go command line tool for Octopus Energy account tasks. It can join eligible Power Down Saving Sessions, use available Wheel of Fortune spins, and show Wheel of Fortune history.

Account changing commands default to a dry run. Help and version output do not require credentials or network access. Commands that contact Octopus require an API key and account number and can run unattended with systemd timers.

## Requirements and credentials

Set these environment variables for commands that contact Octopus:

- `OCTOPUS_API_KEY`
- `OCTOPUS_ACCOUNT_NUMBER`

For local development, copy `.env.example` to `.env` and add the values. The application loads `./.env` when present. Values in the process environment take precedence over values in `.env`.

On a systemd host, the services use `/etc/octo-cli/environment` as their environment file. The application reads environment variables and does not read this system file directly.

Keep credentials private. Never commit `.env` or any file that contains real secrets.

## Commands

```sh
# Print help or the injected build version. Neither needs credentials.
octo-cli --help
octo-cli --version

# Join eligible Saving Sessions. This is a dry run unless --execute is set.
octo-cli saving-sessions join
octo-cli saving-sessions join --execute

# Check available Wheel of Fortune spins, or use them with --execute.
octo-cli wheel spin
octo-cli wheel spin --execute

# Show Wheel of Fortune history, with optional date and fuel filters.
octo-cli wheel history
octo-cli wheel history --from 2026-01-01 --to 2026-01-31 --fuel electricity
```

Use `octo-cli saving-sessions --help` or `octo-cli wheel --help` for group help. `--execute` is available only for `join` and `spin`. History flags are available only for `wheel history`; dates use `YYYY-MM-DD`, and fuel is `electricity` or `gas`.

History retrieves every matching page, sorts the results newest first, and prints timestamp, prize and prize type. It prefers the API's prize display text and uses the raw value only when display text is unavailable, without assigning a currency or points unit. Missing fields and empty history are shown clearly. If any page fails or is incomplete, the command exits with an error and does not print a partial table.

### Saving Sessions behaviour

The join command preserves the existing eligibility checks, including event type, capacity, region, campaign and joined-session status. Eligible candidates, successful joins, dry-run outcomes and the no-eligible-sessions result print to stdout. No eligible sessions is a successful result. When eligible sessions are available, a dry run explains that `--execute` joins them.

### Wheel of Fortune behaviour

The spin command queries the backend for the available allowance for each fuel. Counts use readable fuel names and singular or plural `spin` wording. No available spins is a successful result. When spins are available, a dry run explains that `--execute` uses them. Each spin is followed by a fresh allowance check. The command stops if the count does not decrease, a response is incomplete, or an API request fails. It never attempts more spins per fuel than were available at the start of the run. Spin mutations are not automatically retried because a timeout could mean the server used a spin but the response was lost. A later invocation checks the remaining allowance afresh.

Available spins, dry-run outcomes, successful spin results and no-action results print to stdout. If a later allowance request fails, earlier successful spin results remain in stdout. A successful spin can have no prize value; this is reported as `not returned` rather than treated as a failed spin. Errors print to stderr; successful authentication has no routine stderr message. The raw prize value is printed without an assumed points or currency unit. The command does not redeem Octopoints into account credit.

Requests use `wheelOfFortuneSpinsAllowed` and `spinWheelOfFortune` at `https://api.backend.octopus.energy/v1/graphql/`, following the operations used by [Home Assistant Octopus Energy](https://github.com/BottlecapDave/HomeAssistant-OctopusEnergy/blob/develop/custom_components/octopus_energy/api_client/__init__.py). These operations are covered by mocked API tests; run a dry run against your account before enabling execution.

## Fresh systemd installation

These instructions install the current Linux amd64 release. Download a different build if your host uses another architecture.

### Install the binary and service account

```sh
curl -fL https://github.com/rogerhutchings/octopus-autojoin/releases/latest/download/octo-cli-linux-amd64 \
  -o /tmp/octo-cli
sudo install -m 0755 /tmp/octo-cli /usr/local/bin/octo-cli
rm /tmp/octo-cli

sudo useradd --system --user-group \
  --home-dir /nonexistent \
  --shell /usr/sbin/nologin \
  octo-cli
```

The GitHub repository remains named `octopus-autojoin`; release binaries use the `octo-cli` name.

### Configure credentials

Create the root-owned environment file and enter the existing Octopus credentials with `sudoedit`:

```sh
sudo install -d -m 0755 /etc/octo-cli
sudo touch /etc/octo-cli/environment
sudo chmod 0600 /etc/octo-cli/environment
sudo chown root:root /etc/octo-cli/environment
sudoedit /etc/octo-cli/environment
```

The file must contain:

```ini
OCTOPUS_API_KEY=your-api-key
OCTOPUS_ACCOUNT_NUMBER=A-YOURACCOUNT
```

### Install the Saving Sessions units

Create `/etc/systemd/system/octo-cli-saving-sessions.service`:

```ini
[Unit]
Description=Join eligible Octopus Saving Sessions
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
User=octo-cli
Group=octo-cli
EnvironmentFile=/etc/octo-cli/environment
ExecStart=/usr/local/bin/octo-cli saving-sessions join --execute
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
```

Create `/etc/systemd/system/octo-cli-saving-sessions.timer`:

```ini
[Unit]
Description=Check Octopus Saving Sessions every 20 minutes

[Timer]
OnCalendar=*:00,20,40
Persistent=true
Unit=octo-cli-saving-sessions.service

[Install]
WantedBy=timers.target
```

### Install the Wheel of Fortune units

Create `/etc/systemd/system/octo-cli-wheel.service`:

```ini
[Unit]
Description=Use available Octopus Wheel of Fortune spins
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
User=octo-cli
Group=octo-cli
EnvironmentFile=/etc/octo-cli/environment
ExecStart=/usr/local/bin/octo-cli wheel spin --execute
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
```

Create `/etc/systemd/system/octo-cli-wheel.timer`:

```ini
[Unit]
Description=Check Octopus Wheel of Fortune daily

[Timer]
OnCalendar=*-*-* 09:00:00 Europe/London
RandomizedDelaySec=15m
Persistent=true
Unit=octo-cli-wheel.service

[Install]
WantedBy=timers.target
```

Saving Sessions runs every 20 minutes. The wheel check runs daily between 09:00 and 09:15 UK time. `Persistent=true` allows a missed run to happen when the timer next starts. Systemd prevents overlapping runs of the same service; separate CLI processes do not share a lock.

### Run dry runs, then enable timers

These commands use the service account and credentials, and omit `--execute`:

```sh
sudo systemd-run --wait --pipe --collect \
  --property=User=octo-cli \
  --property=Group=octo-cli \
  --property=EnvironmentFile=/etc/octo-cli/environment \
  /usr/local/bin/octo-cli saving-sessions join

sudo systemd-run --wait --pipe --collect \
  --property=User=octo-cli \
  --property=Group=octo-cli \
  --property=EnvironmentFile=/etc/octo-cli/environment \
  /usr/local/bin/octo-cli wheel spin
```

After both dry runs succeed, load and enable the units:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now octo-cli-saving-sessions.timer
sudo systemctl enable --now octo-cli-wheel.timer
```

Check scheduled runs and logs with:

```sh
systemctl list-timers 'octo-cli-*'
sudo journalctl -u octo-cli-saving-sessions.service
sudo journalctl -u octo-cli-wheel.service
```

Starting either service directly performs account actions because the service commands include `--execute`.

## Migration from octopus-autojoin

Keep fresh installations separate from this section. On an existing host, disable the old timers before enabling the new timers so both schedules do not run together:

```sh
sudo systemctl disable --now octopus-autojoin.timer
sudo systemctl disable --now octopus-wheel-of-fortune.timer
```

Install the renamed binary at `/usr/local/bin/octo-cli`, create the `octo-cli` service user/group, and install the new unit files from the fresh-install section. For the credential step, do not first create an empty new environment file. Transfer credentials without printing them. The following command copies the old environment file only if the new file does not already exist; it does not overwrite an existing destination:

```sh
sudo install -d -m 0755 /etc/octo-cli
sudo sh -c 'test ! -e /etc/octo-cli/environment && install -o root -g root -m 0600 /etc/octopus-autojoin/environment /etc/octo-cli/environment'
```

If you stored credentials elsewhere, use a secure copy method that does not display the file or replace an existing destination. Confirm that the new environment file exists and has mode `0600` without printing its contents:

```sh
sudo test -f /etc/octo-cli/environment
sudo stat -c '%a %U:%G %n' /etc/octo-cli/environment
```

Reload systemd and run the dry runs from the fresh-install section. Then enable the new timers:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now octo-cli-saving-sessions.timer
sudo systemctl enable --now octo-cli-wheel.timer
```

The remote GitHub repository remains `rogerhutchings/octopus-autojoin`; repository renaming and remote changes are manual follow-up steps. Update any external deployment automation to install `octo-cli` and use the new unit names.

## Development

```sh
gofmt -w main.go internal/cli/*.go internal/config/*.go internal/octopus/*.go internal/savingsessions/*.go internal/wheeloffortune/*.go
go test ./...
go vet ./...
go build -o octo-cli .
```

The code is split into:

- `main.go`: process entry point and version injection.
- `internal/cli`: command parsing and dispatch.
- `internal/config`: shared environment and `.env` loading.
- `internal/octopus`: shared Kraken authentication and GraphQL HTTP client.
- `internal/savingsessions`: Saving Sessions queries, eligibility filtering and joining.
- `internal/wheeloffortune`: spin availability, execution and history.

Feature packages share the authenticated client and own their API operations.
