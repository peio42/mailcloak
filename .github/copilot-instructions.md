# Copilot instructions for mailcloak

This repository implements a lightweight mail authorization daemon for Postfix,
integrating an external identity provider and a local SQLite store.
Changes should preserve the simplicity, determinism, and operational clarity of the system.

---

## Architecture overview

- The daemon entrypoint is [cmd/mailcloak/main.go](/cmd/mailcloak/main.go).
  It starts two Unix socket servers (policy and socketmap), then drops privileges
  and runs both concurrently.

- **Policy service** (Postfix policy delegation) lives in
  [internal/mailcloak/policy.go](/internal/mailcloak/policy.go):
  - Authorization decisions are performed at the `RCPT` stage.
  - Primary email lookup is done via the identity resolver, then local SQLite aliases.
  - Sender checks are also handled here, assuming `smtpd_delay_reject = yes`.
  - The `MAIL` stage is intentionally bypassed and usually returns `DUNNO`.

- **Socketmap service** in
  [internal/mailcloak/socketmap.go](/internal/mailcloak/socketmap.go):
  - Serves only the `alias` map.
  - Uses native Postfix socketmap framing (`<len>:<payload>,`).
  - Rewrites aliases to `username@domain`.

- **Identity resolution** is implemented in
  [internal/mailcloak/keycloak.go](/internal/mailcloak/keycloak.go):
  - Uses client-credentials authentication and the Keycloak Admin API.
  - Policy code depends only on an abstract resolver interface.
  - Results may be cached in-memory by the policy layer.

- **SQLite access** is centralized in
  [internal/mailcloak/sqlite.go](/internal/mailcloak/sqlite.go).
  - The database schema is managed by the CLI helper
    [mailcloakctl](/mailcloakctl).

---

## Configuration and runtime conventions

- Configuration is YAML-based and loaded by
  [internal/mailcloak/config.go](/internal/mailcloak/config.go).
  A sample configuration is available under
  [docs/configs/config.yaml.sample](/docs/configs/config.yaml.sample).

- Unix socket paths **must** reside under the Postfix chroot
  (typically `/var/spool/postfix`).
  Ownership and permissions are enforced by
  [internal/mailcloak/socket_perms.go](/internal/mailcloak/socket_perms.go).

- The process is expected to start as root, then drop privileges to
  `daemon.user` via
  [internal/mailcloak/privileges.go](/internal/mailcloak/privileges.go).

- Email addresses are normalized to lowercase before any checks.

- Identity provider failures default to temporary failure (`451`)
  unless explicitly configured otherwise.

---

## Developer workflow

- Common tasks are available via the Makefile:
  `make build`, `make run`, `make test`.

- The CLI helper [mailcloakctl](/mailcloakctl):
  - Initializes and manages the SQLite database.
  - Handles aliases and application credentials.
  - Is written in Python and requires `argon2-cffi`
    (see [requirements.txt](/requirements.txt)).

---

## Integration points

- Postfix integration relies on:
  - `check_policy_service unix:private/mailcloak-policy`
  - `socketmap:unix:private/mailcloak-socketmap:alias`

- Dovecot reads from the same SQLite database for SMTP application authentication.
  Application secrets are stored as Argon2id hashes.

---

## Testing expectations

When modifying or adding behavior:

- Corresponding **unit tests are expected**.
- Unit tests live next to the code under `internal/mailcloak/`.
- Shared helpers (fakes, builders, temporary SQLite setup) belong in
  `internal/mailcloak/testutil/`.

Testing guidelines:
- Prefer table-driven tests.
- Keep tests fast, deterministic, and focused on behavior.
- Avoid heavy test frameworks or external mocking libraries.
- Production APIs should not be expanded solely for testing.

Definition of done:
- `make test` passes.
- New or changed behavior is covered by tests.
