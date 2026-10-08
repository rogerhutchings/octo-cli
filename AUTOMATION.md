# Automation and scheduling

## Overview

`octo-cli` is a one-shot command-line application. It performs one check or action and exits; an external scheduler such as systemd starts it when needed. The repository's systemd schedule is:

| Automation | Frequency | Command |
| --- | --- | --- |
| Saving Sessions | Every 20 minutes | `octo-cli saving-sessions join --execute` |
| Wheel of Fortune | Daily at 04:17 Europe/London | `octo-cli wheel spin --execute --max-spins 1` |

This guide covers manual Linux systemd setup. Deployment may also be managed with Ansible or another configuration-management tool. The Services Box Ansible configuration is maintained separately from this repository.

## Prerequisites

- A Linux host with systemd and network access to Octopus Energy.
- The installed CLI binary at `/usr/local/bin/octo-cli`.
- An Octopus API key and account number.
- Permission to create a system service account and manage files under `/etc/systemd/system` and `/etc/octo-cli`.

The release currently provides a Linux amd64 binary. See [Installation in the README](README.md#installation) for the download and checksum steps.

## systemd setup

Create a dedicated system account and the credentials directory:

```sh
sudo useradd --system --user-group \
  --home-dir /nonexistent \
  --shell /usr/sbin/nologin \
  octo-cli

sudo install -d -m 0755 /etc/octo-cli
sudo touch /etc/octo-cli/environment
sudo chmod 0600 /etc/octo-cli/environment
sudo chown root:root /etc/octo-cli/environment
sudoedit /etc/octo-cli/environment
```

Enter the credentials in `/etc/octo-cli/environment`:

```ini
OCTOPUS_API_KEY=your-api-key
OCTOPUS_ACCOUNT_NUMBER=A-YOURACCOUNT
```

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
ExecStart=/usr/local/bin/octo-cli wheel spin --execute --max-spins 1
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
OnCalendar=*-*-* 04:17:00 Europe/London
Persistent=true
Unit=octo-cli-wheel.service

[Install]
WantedBy=timers.target
```

These services run as the unprivileged `octo-cli` user and group. They receive credentials through systemd's environment file and use the existing service restrictions: no new privileges, a private temporary directory, a read-only system view and no access to home directories.

Before enabling the timers, run both commands without `--execute` under the service account to check configuration and connectivity:

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

Then load and enable the timer units:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now octo-cli-saving-sessions.timer
sudo systemctl enable --now octo-cli-wheel.timer
```

## Scheduling

The Saving Sessions calendar expression, `OnCalendar=*:00,20,40`, starts the service at minutes 00, 20 and 40 of each hour. The Wheel timer uses `OnCalendar=*-*-* 04:17:00 Europe/London`. The named timezone makes the local time explicit and follows GMT in winter and BST in summer.

Both timers set `Persistent=true`. For calendar timers, systemd records the last trigger; if the host was down when a scheduled time passed, the timer starts the service once after it becomes active again. It does not replay every missed interval. This catch-up can therefore run an account-changing command soon after boot or after a timer is re-enabled.

The Wheel command's `--max-spins 1` is a combined electricity-and-gas cap for each invocation. The command checks the available allowance and uses electricity first. The cap does not count previous invocations and does not enforce a per-calendar-day limit. The configured schedule normally starts one invocation per day; manual starts and catch-up runs can start additional invocations. The 04:17 time is an operational preference, not a verified way to improve prize odds.

## Credentials and security

Keep `/etc/octo-cli/environment` owned by `root:root` with mode `0600`. This lets systemd read the credentials while preventing other local users from reading the file. Edit it with `sudoedit`; do not paste real credentials into shell commands, commit them, or include them in logs or support requests.

The service runs as the dedicated `octo-cli` user and group. Its systemd restrictions are listed in the service units above. The application reads environment variables supplied by systemd; it does not read `/etc/octo-cli/environment` itself. Keep the file in systemd's environment file format, with one `NAME=value` entry per line.

## Operations and troubleshooting

Check timer state and upcoming execution times:

```sh
systemctl list-timers 'octo-cli-*'
systemctl status octo-cli-saving-sessions.timer octo-cli-wheel.timer
```

Check service state and recent logs:

```sh
sudo systemctl status octo-cli-saving-sessions.service octo-cli-wheel.service
sudo journalctl -u octo-cli-saving-sessions.service
sudo journalctl -u octo-cli-wheel.service
```

Show failed systemd units:

```sh
systemctl --failed
```

To follow a service log while it runs, add `-f` to the relevant `journalctl` command. To retry a failed service, first review its logs and the account's current state, then start it manually if appropriate:

```sh
sudo systemctl start octo-cli-saving-sessions.service
sudo systemctl start octo-cli-wheel.service
```

**Starting either service manually performs a real account-changing operation** because its `ExecStart` includes `--execute`. Starting the Wheel service can consume an available spin. Use the dry-run commands in the setup section to check connectivity without joining sessions or using spins.

## Operational considerations

- Saving Sessions joins only upcoming sessions that match the account's region and event type and have not already been joined. If there are no eligible sessions, the command succeeds without changing the account.
- The Wheel command checks electricity and gas allowances before it acts. If there are no available spins, it succeeds without taking action.
- `--max-spins 1` limits the combined number of spins in one invocation, with electricity first. A later invocation checks the allowances again.
- Spin mutations are not automatically retried. If a request fails or times out, check the current allowance before starting the command again because the service may have used a spin before the response was lost.
- systemd avoids starting a second copy of the same active service unit, but independently started `octo-cli` processes do not share a lock. Avoid overlapping manual commands and timers if a single coordinated run is required.
