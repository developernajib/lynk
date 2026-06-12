# 01 - Getting started

This walks you from a fresh clone to a running full stack: database, event
bus, two Go services, and the React frontend.

## Prerequisites

| Tool                              | Why                     | Install                                                                                                                                |
| --------------------------------- | ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Go 1.26+                          | the services            | https://go.dev/dl                                                                                                                      |
| Docker + Compose                  | local infrastructure    | Docker Desktop                                                                                                                         |
| Node 20+                          | the frontend            | https://nodejs.org                                                                                                                     |
| buf                               | protobuf lint + codegen | `go install github.com/bufbuild/buf/cmd/buf@latest`                                                                                    |
| sqlc                              | type-safe SQL codegen   | `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`                                                                                  |
| protoc-gen-go, protoc-gen-go-grpc | Go proto plugins        | `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` and `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest` |
| migrate                           | database migrations     | `go install -tags 'postgres,file' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`                                             |
| air                               | hot reload              | `go install github.com/air-verse/air@latest`                                                                                           |
| grpcurl (optional)                | poke gRPC APIs by hand  | `go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest`                                                                        |
| make                              | the command runner      | ships with most systems; on Windows use `choco install make` or run the commands inside the Makefile by hand                           |

## Step 0: make the project yours

The boilerplate's module namespace is `github.com/developernajib/lynk`. Rename
it once, before anything else, so every import and go.mod points at your
project:

```bash
scripts/rename-module.sh github.com/you/yourapp
```

The script requires a clean git tree and rewrites every Go file, go.mod, and
config in one reviewable diff.

## Step 1: infrastructure

```bash
make infra-up
```

This starts PostgreSQL, Redis, and NATS (with JetStream) in Docker. Host
ports are offset on purpose so the stack coexists with anything already on
your machine:

| Service    | In-network address | Host address   |
| ---------- | ------------------ | -------------- |
| PostgreSQL | postgres:5432      | 127.0.0.1:5433 |
| Redis      | redis:6379         | 127.0.0.1:6380 |
| NATS       | nats:4222          | 127.0.0.1:4223 |

The dev URLs use `127.0.0.1` instead of `localhost` deliberately: Windows
resolves `localhost` to the IPv6 `::1` first, and a resident service on that
address can intercept the connection.

## Step 2: database

```bash
make migrate-up   # applies services/core/migrations in order
make db-seed      # creates the dev admin (idempotent)
```

The seeder prints the generated admin password once. Set
`SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD` to choose your own. In
production the password is required, never generated.

## Step 3: configuration

Each service reads environment variables, optionally from a `.env` file in
its directory. Copy the examples and adjust if needed:

```bash
cp services/core/.env.example services/core/.env
cp services/gateway/.env.example services/gateway/.env
```

For local development the defaults work as-is. Without RS256 keys configured,
core generates an EPHEMERAL signing key at startup and logs a warning: tokens
work against core directly but die on restart, and the gateway cannot verify
them until you give both sides a real key pair:

```bash
openssl genrsa -out jwt_priv.pem 2048
openssl rsa -in jwt_priv.pem -pubout -out jwt_pub.pem
# core .env:    JWT_PRIVATE_KEY_PEM + JWT_PUBLIC_KEY_PEM
# gateway .env: JWT_PUBLIC_KEY_PEM (same public key, same JWT_ISSUER)
```

## Step 4: run the services

Hot reload (recommended while developing), one terminal each:

```bash
make dev-core          # core gRPC API on :50051, ops on :9091
make dev-core-worker   # outbox relays, consumers, jobs; ops on :9191
make dev-gateway       # public edge on :8080, ops on :9090
```

Or everything containerized:

```bash
make stack-up
```

## Step 5: the frontend

```bash
cd frontend
npm install
npm run dev
```

Open http://localhost:5173. Register an account, sign in, create a note,
rename it. Every action travels browser → gateway (gRPC-Web) → core (gRPC),
and the note events flow through the outbox to NATS, where the audit
consumer records them.

## Step 6: verify by hand (optional)

```bash
# list services through reflection (dev only)
grpcurl -plaintext localhost:50051 list

# health endpoints
curl http://127.0.0.1:9090/healthz   # gateway liveness
curl http://127.0.0.1:9091/readyz    # core readiness incl. dependencies
```

To call an authenticated core RPC directly (bypassing the gateway), inject
the trusted identity header the gateway would normally set:

```bash
grpcurl -plaintext -H "x-user-id: <uuid>" localhost:50051 example.v1.ExampleService/ListNotes
```

## Where to go next

- New to the layout? Read [02 Architecture](02-architecture.md).
- Building your first feature? Read [03 DDD guide](03-ddd-guide.md) and copy
  `services/core/internal/modules/example`.
- The example module and its proto are scaffolding: delete them once you
  have real modules (drop the module directory, its proto, its migration
  entries going forward, the bootstrap registration, and the gateway route).
