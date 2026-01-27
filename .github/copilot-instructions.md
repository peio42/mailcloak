# Copilot instructions for mailcloak

## Big picture architecture
- The daemon in [cmd/mailcloak/main.go](/cmd/mailcloak/main.go) starts two Unix-socket servers: policy and socketmap, then drops privileges and runs both concurrently.
- Policy service (Postfix policy delegation) lives in [internal/mailcloak/policy.go](/internal/mailcloak/policy.go):
  - `RCPT` stage first checks Keycloak primary email, then SQLite aliases; sender checks are also performed here because of assuming `smtpd_delay_reject = yes`.
  - `MAIL` stage is intentionally bypassed in code (see comments) and typically returns `DUNNO`.
- Socketmap service in [internal/mailcloak/socketmap.go](/internal/mailcloak/socketmap.go) serves only the `alias` map using Postfix framing `<len>:<payload>,` and rewrites alias → `username@domain`.
- Keycloak integration in [internal/mailcloak/keycloak.go](/internal/mailcloak/keycloak.go) uses client-credentials token and admin API (`/admin/realms/.../users`) to resolve username/email; policy caches results in-memory (see `Cache` in policy.go).
- SQLite access is centralized in [internal/mailcloak/sqlite.go](/internal/mailcloak/sqlite.go); schema is created by the CLI helper [mailcloakctl](/mailcloakctl).

## Configuration and runtime conventions
- Config is YAML loaded by [internal/mailcloak/config.go](/internal/mailcloak/config.go); sample at [docs/configs/config.yaml.sample](/docs/configs/config.yaml.sample).
- Socket paths **must** be under the Postfix chroot (typically `/var/spool/postfix`), and ownership/mode are applied via [internal/mailcloak/socket_perms.go](/internal/mailcloak/socket_perms.go).
- The process is expected to start as root and then drop to `daemon.user` using [internal/mailcloak/privileges.go](/internal/mailcloak/privileges.go).
- Email addresses are normalized to lowercase before checks (see policy/socketmap code paths).
- Keycloak failure mode defaults to `tempfail` (451) unless configured to `dunno`.

## Developer workflows
- Build/run/test via Makefile: `make build`, `make run`, `make test` (see [Makefile](/Makefile)).
- Install binary with `make install` (installs to `/usr/local/sbin`).
- Initialize SQLite DB and manage aliases/apps with [mailcloakctl](/mailcloakctl) (Python, requires `argon2-cffi` from [requirements.txt](/requirements.txt)).

## Integration points to keep in mind
- Postfix policy service uses `check_policy_service unix:private/mailcloak-policy` and socketmap uses `socketmap:unix:private/mailcloak-socketmap:alias` (see README).
- Dovecot reads the same SQLite DB for app authentication; app secrets are stored as Argon2id hashes by `mailcloakctl`.

## Testing requirements

When modifying or adding Go code in this repository:
- Always add or update **unit tests** for the affected behavior.
- Place unit tests as `*_test.go` files **next to the code** under `internal/mailcloak/`.
- Shared test helpers (fakes, builders, temporary databases) must go under:
  `internal/mailcloak/testutil/`.

### Testing principles
- Prefer **table-driven tests** with clear subtest names.
- Keep tests **lightweight, fast, and deterministic**.
- Do not introduce heavy test frameworks or external mocking libraries.
  Use only the Go standard library unless already required by the module.
- Avoid over-mocking: test behavior, not implementation details.
- Do not change production APIs solely for testing unless strictly necessary.

### Coverage expectations
- Policy logic (MAIL FROM / RCPT TO authorization)
- Socketmap behavior and protocol formatting
- Identity resolution (Keycloak or compatible resolver) using fakes or httptest
- SQLite store behavior using a temporary on-disk database

### Definition of done
- `go test ./...` must pass.
- New behavior must be covered by tests.