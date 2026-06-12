# 07 - Operations

Running, changing, observing, and deploying the system. `make help` lists
every target mentioned here.

## Daily development

```bash
make infra-up            # postgres + redis + nats containers
make dev-core            # hot-reloaded API server     (terminal 1)
make dev-core-worker     # hot-reloaded worker         (terminal 2)
make dev-gateway         # hot-reloaded edge           (terminal 3)
cd frontend && npm run dev                            # (terminal 4)
```

Air rebuilds on every save of a `.go` or `.env` file. The full containerized
stack (`make stack-up`) is for integration checks, not the inner loop:
container rebuilds are much slower than Air.

Quality gates, the same ones CI runs:

```bash
make build vet lint      # per-service compile, vet, golangci-lint v2
make fmt                 # gofmt everything
make hooks               # install pre-commit (hygiene + gitleaks + go checks)
```

## Code generation

```bash
make generate            # buf generate + sqlc generate (core)
cd frontend && npm run generate   # TypeScript clients from the same protos
```

Generated code (`internal/gen`, `frontend/src/gen`) is committed so builds
never depend on generators being installed, and it is never hand-edited or
linted. Changed a `.proto` or a query? Regenerate and commit the result
together with the source change.

## Database changes

```bash
make migrate-create NAME=add_thing   # creates NNNN_add_thing.{up,down}.sql
make migrate-up                      # apply pending
make migrate-down                    # roll back ONE
make migrate-status                  # current version
make db-fresh                        # DESTRUCTIVE: drop + reapply (dev only)
make db-seed                         # dev admin account (idempotent)
```

Migration discipline:

- Never edit a migration that has been applied anywhere. Write a new one.
- Every up has a working down.
- For zero-downtime production changes use expand/contract: add nullable,
  backfill, switch readers, drop in a LATER migration once nothing reads the
  old shape.
- sqlc validates every query against the migrations at generate time, so a
  schema/query mismatch fails at your desk, not in production.

## Ports

| Process                 | Purpose                        | Port                  |
| ----------------------- | ------------------------------ | --------------------- |
| gateway                 | public edge (gRPC-Web + HTTP)  | 8080                  |
| gateway                 | ops: /healthz /readyz /metrics | 9090                  |
| core                    | gRPC API                       | 50051                 |
| core                    | ops                            | 9091                  |
| core-worker             | ops                            | 9191 (API port + 100) |
| postgres / redis / nats | host-mapped infra              | 5433 / 6380 / 4223    |

Infra host ports are offset from the defaults so the stack coexists with
anything already on the machine; inside the compose network the standard
ports apply.

## Health and readiness

- `/healthz` answers if the process is alive. Orchestrators restart on
  failure.
- `/readyz` checks real dependencies (postgres ping, redis ping, NATS
  connection). Orchestrators pull the instance from rotation on failure
  without restarting it, so a database blip never causes a restart storm.
- Containers run a static probe binary (`/healthcheck <url>`) because
  distroless images have no shell; compose gates the gateway's start on
  core's readiness with it.

## Observability

Services emit OpenTelemetry traces and metrics and structured JSON logs.
The contract with the outside world is ONE address:

```
OTEL_ENABLED=true
OTEL_ENDPOINT=<your-collector>:4317   # any OTLP/gRPC collector
OTEL_TRACE_SAMPLE_RATIO=0.1           # sample at volume; 1.0 in dev
```

Telemetry fails open: if the collector is down, spans drop silently and the
service keeps serving. Independent of the collector, every ops port serves
Prometheus metrics at `/metrics`, so a plain Prometheus can always scrape.

What to expect when you look:

- One trace per request, spanning gateway → core → SQL statements → outbox
  → worker relay → consumers (the envelope carries the trace context across
  the bus).
- Every log line carries `request_id`, `trace_id`, and `span_id` for
  log/trace navigation, plus `service`, `env`, `version`.
- Application errors log ONCE with a stable `error.fingerprint`: group by
  that field in your log store and you have error tracking (top issues,
  first seen, regressions) without a separate error product.
- Logging is exception-only by design (errors and slow requests); request
  counts and latencies belong to metrics. Do not add per-request info logs
  to hot paths.

## CI

`.github/workflows/ci.yml` runs on every push and PR:

| Job                     | What                                                        |
| ----------------------- | ----------------------------------------------------------- |
| go (matrix per service) | build, vet, govulncheck, golangci-lint                      |
| proto                   | buf lint always, buf breaking against main on PRs           |
| secrets                 | gitleaks over the full history                              |
| images (matrix)         | docker build + Trivy scan, failing on fixable HIGH/CRITICAL |

Dependabot files weekly grouped update PRs (Go per service, npm, Docker,
Actions). Treat them as reviews, read the changelogs, and merge greenness is
not a substitute for reading a major bump.

## Deploying

The compose file is the local environment, not the production manifest, but
it encodes everything a real deployment needs to reproduce:

- Build each service's Dockerfile (multi-stage, distroless, nonroot,
  `/server` + `/worker` + `/healthcheck`). The worker is the same image with
  the entrypoint overridden to `/worker`.
- Run migrations as a deploy step before the new version serves traffic
  (`migrate -path ... up` from CI/CD, never from inside the service).
- Provide per-service environment from your secret manager; the services
  validate it and refuse to boot on missing values. Production REQUIRES the
  RS256 key pair and a stated seed admin password.
- Scale the API and the worker independently: relays and consumers are
  leader-safe (`FOR UPDATE SKIP LOCKED`, durable consumers, Redis leases),
  so N worker replicas fire each job and relay each event once.
- Put TLS in front: either terminate at a proxy/load balancer (set
  `X-Forwarded-Proto`) or hand the gateway certs via `HTTP_TLS_*`. Enable
  internal mTLS with the `INTERNAL_TLS_*` trio on the gateway and
  `GRPC_TLS_*` on backends.
- Shutdown is graceful by default: on SIGTERM each process stops intake,
  drains in flight work LIFO, and exits; give orchestrators a grace period
  of at least 25 seconds (the drain budget is 20).

## Load baseline

`load-tests/example-notes.js` is a k6 gRPC script with latency thresholds.
It measures a SINGLE instance's baseline so regressions show up; cluster
throughput is a deployment property. Run it against a dedicated environment,
never your laptop stack, when the numbers matter.

## Troubleshooting notes that will save you an hour

- **gopls/IDE shows phantom errors on generated code or fresh packages**
  while `go build` passes: trust the compiler, restart the language server.
- **Windows + localhost**: connection refused or auth failures against
  Dockerized infra often mean something else owns the port on `::1` (WSL
  services intercept localhost). Use `127.0.0.1` and the offset ports.
- **JetStream "no response from stream"** when publishing: the stream owner
  has not run yet. Start the owning worker (it declares the stream) before
  consumers and relays that depend on it.
- **A consumer reprocesses old events after redeploy**: durable consumers
  resume from the last ACKED message. If you see reprocessing, something
  Nak'd or crashed before Ack; check the consumer's error log and its
  idempotency strategy.
- **Compose port conflict on 5433/6380/4223**: another stack uses the same
  offsets; change the host side of the mapping only, never the container
  side.
- **`migrate` says "unknown driver postgres"**: reinstall it with the build
  tags: `go install -tags 'postgres,file' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`.
