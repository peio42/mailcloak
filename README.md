# mailcloak

Postfix policy + socketmap daemon that validates recipients/senders against Keycloak and serves a local aliases SQLite database.

## What it does
- **Policy service** (Postfix policy delegation):
  - `RCPT` stage: accepts if the recipient exists in Keycloak (primary email) or as a local alias in SQLite.
  - `MAIL` stage (authenticated submissions): accepts only if the sender is the user’s primary Keycloak email or one of their aliases.
- **Socketmap service**: exposes an `alias` map to Postfix, rewriting alias -> `username@domain`.

## Project layout
- `cmd/mailcloak/` – main package entrypoint
- `internal/mailcloak/` – daemon sources
- `go.mod` / `go.sum` – Go module files
- `configs/config.yaml.sample` – sample config to copy to `/etc/mailcloak/config.yaml`
- `configs/openrc-mailcloak` – OpenRC service file
- `db-init.sql` – SQLite schema (also auto-created by the app)
- `mailcloakctl` – CLI helper to manage aliases

## Build the binary
From the repository root:

```bash
make build
```

To install system-wide:

```bash
make install
```

To run locally:

```bash
make run
```

## Configuration
Copy the sample config and edit it:

```bash
install -d -m 0750 -o root -g postfix /etc/mailcloak
cp configs/config.yaml.sample /etc/mailcloak/config.yaml
```

Key settings:
- `keycloak.*` must point to your Keycloak realm and a client with permission to query users.
- `policy.domain` is the email domain enforced by the policy.
- `sqlite.path` is the aliases database path.
- `sockets.*` must be under the Postfix chroot (usually `/var/spool/postfix`).

## Mailcloak database
You can manage aliases using the helper script:

```bash
./mailcloakctl aliases add alias@example.com username
./mailcloakctl aliases list
```

The script creates the schema automatically if missing.

## Postfix integration (example)
Policy service (smtpd_recipient_restrictions):
```
check_policy_service unix:private/mailcloak
```

Socketmap (virtual_alias_maps):
```
socketmap:unix:private/mailcloak-socketmap:alias
```

## OpenRC
Use the provided service file:

```bash
cp configs/openrc-mailcloak /etc/init.d/mailcloak
rc-update add mailcloak default
rc-service mailcloak start
```

## Notes
- If Keycloak is unavailable, the policy returns `451` by default (configurable via `policy.keycloak_failure_mode`).
- The policy caches lookups for `policy.cache_ttl_seconds`.
