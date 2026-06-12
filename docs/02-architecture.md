# 02 - Architecture

## The big picture

lynk is two deployable Go services plus a frontend, with PostgreSQL, Redis,
and NATS JetStream underneath:

```
 Browser (React + gRPC-Web)
    │
    ▼
 ┌─────────────────────────────────────────────┐
 │ gateway :8080                               │
 │  edge middleware chain → gRPC-Web bridge    │
 │  (verifies JWT ONCE, injects identity)      │
 └───────────────┬─────────────────────────────┘
                 │ plain gRPC (+ mTLS in prod)
                 ▼
 ┌─────────────────────────────────────────────┐     ┌──────────────┐
 │ core :50051  (modular monolith)             │────▶│ PostgreSQL   │
 │  modules: identity · authz · audit · example│     │ (per-module  │
 │  each module: own schema + own outbox       │     │  schemas)    │
 └───────────────┬─────────────────────────────┘     └──────────────┘
                 │ outbox rows
                 ▼
 ┌─────────────────────────────────────────────┐     ┌──────────────┐
 │ core-worker                                 │────▶│ NATS         │
 │  outbox relays → publish                    │     │ JetStream    │
 │  consumers (audit ledger) ← subscribe       │◀────│ CORE_EVENTS  │
 │  scheduled jobs (leader-leased via Redis)   │     └──────────────┘
 └─────────────────────────────────────────────┘
```

Redis serves the gateway (rate limiting, token blacklist), identity (login
lockout, API key cache), and the jobs kit (leader leases).

## Why this shape

- **One gateway** so every cross-cutting security control exists exactly
  once, at the entrance. Backend services stay lean and trust the identity
  headers the gateway injects, protected by internal mTLS.
- **One core service as a modular monolith** because most features do not
  need their own deployable. They need isolation, which modules provide,
  without the operational cost of another binary, database, and pipeline.
  When a module outgrows the monolith, [04](04-modular-monolith.md) shows
  the mechanical extraction.
- **A separate worker binary per service** so background throughput (relays,
  consumers, jobs) and API latency scale independently and a busy consumer
  can never starve request handlers. Same module, same code, different
  process.
- **Events through a transactional outbox** because publishing to the bus
  inside a database transaction is the only way to guarantee that state
  changes and the events describing them agree. See the eventing section
  below.

## Repository layout

```
lynk/
├── services/
│   ├── core/                  the modular monolith
│   │   ├── cmd/server         gRPC API binary
│   │   ├── cmd/worker         relays + consumers + jobs binary
│   │   ├── cmd/seed           dev admin seeder
│   │   ├── cmd/healthcheck    container probe
│   │   ├── internal/platform/ shared plumbing (see below)
│   │   ├── internal/modules/  feature modules (identity, authz, audit, example)
│   │   ├── internal/bootstrap composition root: wiring, lifecycles
│   │   ├── internal/gen/      GENERATED code (committed, never edited)
│   │   ├── proto/             this service's API contracts
│   │   ├── migrations/        versioned schema, applied with migrate
│   │   └── sqlc/queries/      raw SQL compiled by sqlc
│   └── gateway/               the public edge
│       ├── internal/edge/     middleware chain + gRPC-Web bridge
│       └── internal/platform/ its own copy of the plumbing it needs
├── frontend/                  React + Vite + generated gRPC-Web clients
├── deploy/docker-compose.yml  local stack
├── docs/                      you are here
├── load-tests/                k6 baseline scripts
└── scripts/                   migrate helpers, rename-module, pre-commit
```

Each service has its OWN go.mod, proto directory, migrations, Dockerfile, and
platform copy. There is no shared Go module and no go.work on purpose: shared
code couples deployables together, and the coupling always grows. The price
is some duplication between platform copies; the payoff is that services can
be built, upgraded, and deployed in total isolation.

## The platform packages (core's `internal/platform`)

| Package                                   | Provides                                                                                                                                           |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `config`                                  | typed env readers, .env loading, fail-fast validation, the central Config                                                                          |
| `logger`                                  | zerolog setup + request-scoped logger through context                                                                                              |
| `grpcserver`                              | hardened server: recovery, request-id, exception-only logging, error mapping with fingerprints, timeouts, protovalidate, keepalive, env-gated mTLS |
| `postgres`                                | write/read pools (replica round-robin), transaction manager with nesting-aware join                                                                |
| `redis`                                   | traced client with startup ping                                                                                                                    |
| `nats`                                    | JetStream with the stream OWNERSHIP split: owners declare (EnsureStream), consumers bind (EnsureConsumer)                                          |
| `telemetry`                               | OpenTelemetry traces + metrics, W3C propagation, Prometheus fallback on the ops port                                                               |
| `health`                                  | /healthz vs /readyz ops server with dependency checks                                                                                              |
| `auth`                                    | the Principal type + interceptors that read gateway-injected identity                                                                              |
| `jwt`                                     | RS256 signer + verifier sharing ONE config so the issuer can never drift                                                                           |
| `secure`                                  | crypto/rand tokens, OTP codes, SHA-256 token hashing, UUIDv7, argon2id passwords                                                                   |
| `cache`                                   | generic two-tier cache: in-process Ristretto + Redis, singleflight stampede protection                                                             |
| `jobs`, `lock`                            | leader-leased scheduled jobs, Redis locks with ownership tokens                                                                                    |
| `breaker`                                 | circuit breaker for flaky dependencies                                                                                                             |
| `apperror`                                | transport-agnostic error type with stable fingerprints for log grouping                                                                            |
| `shutdown`, `runtimeenv`, `safe`, `clock` | LIFO graceful drain, container-aware runtime tuning, panic-safe goroutines, injectable time                                                        |

## Life of a request

1. The browser calls `example.v1.ExampleService/UpdateNote` over gRPC-Web.
2. The gateway's middleware chain runs in fixed order: recovery, request-id,
   logging, HTTPS redirect, security headers, gzip, CORS, body limit,
   timeout, global and per-IP rate limits, CSRF, then authentication. Auth
   strips any client-supplied identity headers, verifies the JWT signature
   locally (no network call), checks the revocation Bloom filter, and
   injects `x-user-id`, `x-role`, `x-token-type`.
3. The bridge translates gRPC-Web to plain gRPC and the transparent proxy
   forwards raw frames to core, passing ONLY allowlisted metadata.
4. Core's interceptor chain runs (recovery, request-id, logging, error
   mapping, timeout, validation), rebuilds the Principal from the trusted
   headers, and dispatches to the example module's handler.
5. The handler consults the ABAC checker, then calls the use case. The use
   case runs the domain logic and persists the aggregate AND its events in
   one transaction (the outbox).
6. The worker's relay later claims the outbox row, publishes it to
   JetStream, and the audit consumer records it in the ledger.

One OpenTelemetry trace spans all of it: the gateway starts the root span,
otelgrpc carries it to core, the outbox envelope carries it to the worker,
and the consumer continues it. Every log line carries the trace id.

## Life of an event

Producers never call the bus directly. A use case writes an outbox row in
the same transaction as the state change. The worker relay claims batches
with `FOR UPDATE SKIP LOCKED` (multiple worker replicas never double-claim),
publishes to JetStream, and marks rows published only after the broker acks.
Delivery is therefore at-least-once, and every consumer declares how it
copes (see the idempotency menu in [03](03-ddd-guide.md)).

Stream ownership is a hard rule: exactly one service declares a stream's
configuration (core's worker declares CORE_EVENTS and its subject list in
`internal/bootstrap/modules.go`). Everyone else binds to it. Two services
"ensuring" one stream with different subject lists silently diverges on
every restart, so the nats package makes the mistake impossible: consumers
get a bind-only API.

## Configuration model

Every setting is an environment variable, loaded once at startup and
validated fail-fast (the service refuses to boot with a missing required
value rather than crashing on first use). Subsystem packages own small
Config structs; `internal/platform/config/service.go` holds the central
typed Config that bootstrap maps onto them. `.env` files are a development
convenience; the real environment always wins.
