# octopus-autojoin

`octopus-autojoin` requires these environment variables:

- `OCTOPUS_API_KEY`
- `OCTOPUS_ACCOUNT_NUMBER`

For local development, copy `.env.example` to `.env` and fill in your values.
The application optionally loads `./.env`; it is also fine to set the variables
directly in your shell. Values already present in the process environment take
precedence over values in `.env`.

In production, systemd can provide the same variables with an
`EnvironmentFile=/etc/octopus-autojoin/environment` service setting. The path
is part of the service deployment configuration; the application reads only
environment variables.

Keep credentials private. Never commit `.env` or any file containing real
secrets.
