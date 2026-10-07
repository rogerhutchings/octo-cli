# AGENTS.md

## Project

`octo-cli` is a small Go CLI for Octopus Energy account tasks: Saving Sessions,
Wheel of Fortune spins and spin history. It runs once per invocation; systemd
timers provide scheduling outside the application.

The project is being renamed from `octopus-autojoin`. Follow the current task
when working across that transition. Do not treat planned commands as already
implemented, or implement roadmap features without a request.

## Working approach

- Read the relevant code, tests and README before changing behaviour.
- Keep changes focused on the requested task. Preserve unrelated local work.
- Reuse shared code where there is an actual common responsibility; avoid
  speculative abstractions and broad rewrites.
- Use descriptive names, idiomatic Go and standard-library facilities where
  practical. Add dependencies only when they offer a clear benefit.
- Use British English in documentation and user-facing prose.
- Explain material implementation choices and report any unresolved limitations.
- Do not commit, push, tag, release, rename remote repositories or deploy unless
  explicitly requested. Routine local edits and tests do not need confirmation.
- Do not change the separate Ansible deployment repository or running services
  as part of a CLI-only task.

## Architecture

- Keep `main.go` small: process setup, version injection and exit handling.
- Put command parsing, validation, help and dispatch in `internal/cli` as part
  of the command restructure.
- Keep configuration loading in `internal/config` and shared HTTP, GraphQL and
  authentication code in `internal/octopus`.
- Keep feature behaviour in `internal/savingsessions` and
  `internal/wheeloffortune`.
- Reuse fetching and eligibility logic across actions and read-only commands.
- Pass contexts, HTTP clients and output writers where needed for testability.
  Avoid global mutable dependencies and unnecessary interfaces.

## CLI contract

The agreed command interface after the restructure is:

```text
octo-cli saving-sessions join [--execute]
octo-cli saving-sessions list
octo-cli wheel spin [--execute]
octo-cli wheel status
octo-cli wheel history [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--fuel electricity|gas]
```

- Bare root and group commands show help and exit successfully.
- Support `--help` at every level and `--version` at the root without loading
  credentials or making network requests.
- Flags follow the leaf command and apply only to that command.
- Reject invalid commands, arguments, filters and date ranges before loading
  credentials or making requests.
- Account-changing commands default to a dry run and require `--execute` to
  change account state. Read-only commands reject `--execute`.
- `saving-sessions list` shows upcoming sessions and their eligibility reasons;
  `wheel status` shows available electricity and gas spins. Both are read-only.
- Results belong on stdout; diagnostic logs and errors belong on stderr.
- No eligible sessions, no available spins and empty history are successful
  outcomes, with clear output.
- Preserve exit codes: 0 for success/help, 2 for usage errors, 1 for operational
  failures.
- Old flat commands and the implicit Saving Sessions action need no aliases
  after the agreed breaking restructure.
- JSON output and scratchcards are separate future tasks. Do not add
  placeholders or unsupported commands.

## Configuration and credentials

- Use `OCTOPUS_API_KEY` and `OCTOPUS_ACCOUNT_NUMBER`; preserve these names through
  the project rename.
- Preserve environment-over-dotenv precedence. A missing `.env` is fine when
  environment variables provide the required configuration.
- Keep dotenv loading simple and report loading/parsing failures. Reject empty
  or whitespace-only required credentials before making requests.
- Never commit credentials or print API keys, tokens, credential fragments,
  authentication headers or request bodies. Use dummy values in tests.
- Preserve exact credential redaction in errors. Do not introduce arbitrary
  substring redaction that can corrupt useful error messages.
- Make errors concise and actionable, identify the failed stage, and do not
  present an ambiguous upstream rejection as a confirmed diagnosis.

## API and account behaviour

- Preserve established endpoints and authentication conventions unless the task
  requires a verified change.
- Verify unfamiliar API fields and semantics using existing code, authoritative
  documentation or read-only schema introspection. Do not invent API fields.
- Use bounded HTTP timeouts and propagate context cancellation.
- Treat HTTP failures, GraphQL errors and missing required response data as
  errors. Do not silently accept partial GraphQL results.
- Do not automatically retry account-changing mutations: a failed response
  does not establish that the operation was not applied.
- Do not perform live spins, joins, claims or other account mutations merely
  to test code. Use mocks unless the user explicitly requests the live action.
- Preserve Saving Sessions eligibility, region and event-type filtering and
  existing joined-session handling.
- For wheel spins, use the API allowance rather than a hard-coded monthly
  assumption. Preserve the initial spin budget and allowance rechecks after
  each spin; stop on errors or an allowance that fails to decrease.
- History must follow pagination, detect missing pagination metadata and
  repeated cursors, and fetch all matching pages before printing a table.
  An incomplete fetch must fail without presenting partial history as complete.
- Handle nullable fields explicitly. Prefer API prize display text; do not
  infer pounds or points from a raw numeric value.
- Do not assume history ordering, retention or per-record fuel information.

## Validation

For Go code changes, format the changed files and run:

```sh
go test ./...
go vet ./...
```

- Add focused tests for changed behaviour and realistic failure modes, using
  mocked HTTP responses rather than live accounts.
- Preserve tests for dry runs, filtering, mutation safeguards, pagination,
  empty results and credential redaction.
- For CLI changes, test routing, command-specific flags, help/version without
  credentials, and validation before network access.
- For build or rename changes, build the binary and smoke-test help, version
  and the release workflow's version injection.
- Documentation-only changes do not require new Go tests.
- Report exactly which checks ran and their results. Distinguish mocked tests,
  code review and live API checks; never imply an unperformed check passed.

## Documentation and deployment examples

- Keep README usage, help output and actual behaviour aligned.
- Installation and scheduling examples must work from a fresh install; keep
  migration instructions separate.
- After the rename, use `/usr/local/bin/octo-cli`, service user/group `octo-cli`,
  and `/etc/octo-cli/environment`.
- Use `octo-cli-saving-sessions.service/.timer` and
  `octo-cli-wheel.service/.timer` for the replacement units.
- Preserve agreed schedules and service hardening. Migration instructions must
  disable old timers before enabling replacements and preserve credentials
  without displaying or overwriting them.
- Keep module imports, binary names, release assets and build instructions
  consistent. Preserve release targets and version injection unless requested.

## Commit messages

When asked for a commit message, use an imperative subject followed by:

```text
# If applied, this commit will...
Describe the resulting behaviour.

# Why is this change needed?
Prior to this change, ...

# How does it address the issue?
This change ...

# Provide links to any relevant tickets, articles or other resources
Relevant links, or N/A.
```
