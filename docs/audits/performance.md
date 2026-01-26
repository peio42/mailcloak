# Extent of heavy‑load readiness (current state)

- Designed as a single in‑process policy + socketmap daemon per Postfix host. Concurrency is goroutine‑per‑connection in [policy.go:59](/internal/mailcloak/policy.go#59) and [socketmap.go:34](/internal/mailcloak/socketmap.go#34), which is fine for moderate load.
- Throughput is tightly coupled to Keycloak admin API latency and availability. Each cache miss triggers a token request plus admin lookup in [keycloak.go:32](/internal/mailcloak/keycloak.go#32) and [keycloak.go:76](/internal/mailcloak/keycloak.go#76).
- SQLite is used for aliases/apps. Reads are fast, but it is single‑node and not meant for shared, multi‑host, high‑write workloads ([sqlite.go:16](/internal/mailcloak/sqlite.go#16)).

# Key points to watch out for

- **Data race in cache**: [policy.go:14](/internal/mailcloak/policy.go#14) uses a plain map shared across goroutines without a mutex. Under load it can corrupt or panic.
- **Unbounded cache growth**: the cache map never evicts, so high cardinality can grow memory.
- **Keycloak load amplification**: every cache miss obtains a new token and hits admin endpoints ([keycloak.go:137](/internal/mailcloak/keycloak.go#137), [keycloak.go:107](/internal/mailcloak/keycloak.go#107)). This will bottleneck at scale.
- **Single‑node state**: SQLite in [sqlite.go:16](/internal/mailcloak/sqlite.go#16) is local. For multi‑host Postfix, alias/app state isn’t shared.
- **Error handling under load**: transient accept errors cause the servers to return and crash (via log.Fatalf in [main.go:16](/internal/mailcloak/main.go#16)).
- **No read deadlines**: socket handlers do not set deadlines, so a stalled client can tie up goroutines indefinitely ([policy.go:85](/internal/mailcloak/policy.go#85), [socketmap.go:61](/internal/mailcloak/socketmap.go#61)).
- **Log volume**: per‑request logging in hot paths can become a CPU+I/O bottleneck ([policy.go:85](/internal/mailcloak/policy.go#85), [socketmap.go:61](/internal/mailcloak/socketmap.go#61)).
- **Scale model**: the design expects local UNIX sockets as per config sample config.yaml.sample. For large scale, each Postfix node must run its own instance.

# Changes required for large‑scale production integration

1. **Make cache safe and bounded**
  - Protect [policy.go:14](/internal/mailcloak/policy.go#14) with `sync.RWMutex` or replace with `sync.Map`.
  - Add size limits/eviction (LRU) and periodic cleanup.
2. **Cache Keycloak tokens**
  - Persist token + expiry in keycloak.go:15 to avoid a token request per lookup.
  - Add circuit‑breaker/backoff around admin API calls.
3. **Reduce Keycloak dependency**
  - Increase cache TTL or introduce a shared cache (Redis) for `email_exists`/`email_by_username`.
  - Consider batch lookups or a local directory mirror if using Keycloak at scale.
4. **Scale out the DB**
  - Replace SQLite with Postgres/MySQL for shared alias/app state across Postfix nodes.
  - Add migrations and connection pooling limits (db.SetMaxOpenConns etc.) in sqlite.go:16.
5. **Harden server loop**
  - In [policy.go:59](/internal/mailcloak/policy.go#59) and [socketmap.go:34](/internal/mailcloak/socketmap.go#34), treat transient accept errors as retryable.
  - Add read/write deadlines on connections.
6. **Improve observability**
  - Add metrics (Prometheus), health checks, and structured logs; keep request logs sampling or at debug level.
7. **Refine deployment model**
  - Run as a sidecar or local daemon per Postfix node (current UNIX socket model).
  - For centralized service, add TCP listeners with mTLS and strict ACLs, and update Postfix accordingly.

# Bottom line
As‑is, it is suited for small to mid‑sized deployments with moderate mail volume. For heavy workloads and multi‑host production platforms, the main blockers are cache concurrency safety, Keycloak call amplification, lack of shared state beyond a single node, and resiliency/observability gaps. Addressing the items above will be required before operating at large scale.