# lynk

A production-ready Go boilerplate you clone to start a real project. It ships
a gateway with a hardened HTTP edge and a gRPC-Web bridge, a modular-monolith
core service built with DDD that extracts into microservices mechanically, a
React frontend, and the operational glue around them: migrations, seeding,
hot reload, Docker, CI, and supply-chain scanning.

## What you get out of the box

| Area          | Included                                                                                                                                                                                             |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Edge          | Security headers with CSP nonce, CORS allowlist, CSRF double-submit, 3-level sliding-window rate limiting, HTTPS redirect, gzip, body limits, request correlation                                    |
| Auth          | Argon2id passwords, RS256 access tokens, rotating opaque refresh tokens, account lockout, logout with token blacklisting (Bloom-filter fronted), API keys, OTP password reset and email verification |
| Authorization | ABAC policy engine: CEL expressions stored in the database, deny by default, runtime editable                                                                                                        |
| Data          | PostgreSQL with sqlc, write/read pool split with replica round-robin, optimistic locking, transactional outbox                                                                                       |
| Eventing      | NATS JetStream, outbox relays, durable consumers, an audit ledger built by subscription                                                                                                              |
| Observability | OpenTelemetry traces and metrics end to end (browser request to event consumer), Prometheus fallback, structured logs with trace correlation                                                         |
| Frontend      | React + Vite + TypeScript with generated gRPC-Web clients                                                                                                                                            |
| Ops           | Docker Compose, distroless images with healthchecks, Makefile, Air hot reload, GitHub Actions CI, gitleaks, Dependabot, Trivy                                                                        |

## Start here

1. [Getting started](docs/01-getting-started.md): from clone to a running stack.
2. [Architecture](docs/02-architecture.md): how the pieces fit.
3. [The DDD guide](docs/03-ddd-guide.md): how to build features the way this codebase expects.
4. [Modular monolith](docs/04-modular-monolith.md): the module rules and how extraction stays cheap.
5. [Adding a service](docs/05-adding-a-service.md): a full walkthrough using an order service.
6. [Security](docs/06-security.md): the threat model and every control.
7. [Operations](docs/07-operations.md): running, migrating, observing, deploying.

## Quick start

```bash
scripts/rename-module.sh github.com/you/yourapp   # optional, do it first
make infra-up                                     # postgres + redis + nats
make migrate-up
make db-seed                                      # creates the dev admin
make dev-core                                     # terminal 1
make dev-core-worker                              # terminal 2
make dev-gateway                                  # terminal 3
cd frontend && npm install && npm run dev         # terminal 4
```

Open http://localhost:5173, register a user, write a note.
