# 05 - Adding a service, worked end to end: an order service

This is the complete recipe for adding a new deployable, executed against a
realistic example: an `order` service owning carts and orders with a status
state machine. Follow it literally and substitute your domain.

**First, the gate:** does this need to be a service at all? Re-read
[04](04-modular-monolith.md). An order feature for a small product belongs
in core as a module. The service split is justified when order intake has
its own scaling axis (bursty checkout traffic), its own failure isolation
needs (order intake must survive a core outage), or its own data lifecycle.
This guide assumes you have crossed that gate.

## What you will build

```
services/order/
├── cmd/server/main.go            gRPC API on :50052
├── cmd/worker/main.go            outbox relay + consumers
├── cmd/healthcheck/main.go       container probe
├── internal/platform/            copied from core, trimmed
├── internal/bootstrap/           this service's composition root
├── internal/order/               the domain (hexagon layout)
│   ├── domain/                   Order aggregate + state machine
│   ├── application/              PlaceOrder, AdvanceStatus, ...
│   ├── adapter/grpc/
│   └── infrastructure/
├── proto/order/v1/order.proto
├── migrations/
├── sqlc/queries/order.sql
├── buf.yaml · buf.gen.yaml · sqlc.yaml
├── Dockerfile · .env.example · .air.toml · .golangci.yml
└── go.mod                        module github.com/you/yourapp/services/order
```

## Step 1: skeleton

```bash
cd services
mkdir -p order/cmd/server order/cmd/worker order/cmd/healthcheck \
         order/internal/bootstrap order/internal/order \
         order/proto/order/v1 order/migrations order/sqlc/queries

# the platform is copied per service, never shared (see 04 for why)
cp -r core/internal/platform order/internal/platform

# tooling configs are identical in shape
cp core/buf.yaml core/buf.gen.yaml core/sqlc.yaml core/.golangci.yml \
   core/.air.toml core/Dockerfile core/.env.example order/

cd order && go mod init github.com/you/yourapp/services/order
```

Now fix the copies:

- Every `.go` file under `order/internal/platform`: replace the import path
  segment `services/core/internal` with `services/order/internal`
  (`scripts/rename-module.sh` shows the sed pattern; scope it to the new
  directory).
- Trim platform packages the service will not use. An order service keeps
  config, logger, grpcserver, postgres, redis, nats, telemetry, health,
  auth, jwt (verify side), secure, shutdown, runtimeenv, safe, apperror,
  clock; it can drop cache, jobs, lock, breaker until needed.
- `order/.env.example` and the central config: new defaults
  `APP_NAME=order`, `GRPC_PORT=50052`, `METRICS_PORT=9092`,
  `DB_WRITE_URL=...order_db...`.
- `.air.toml`: nothing to change (it builds `./cmd/server`).
- `Dockerfile`: nothing to change (same three-binary layout). Remove the
  `/worker` build line only if the service genuinely has no worker.
- `cmd/healthcheck/main.go`: copy from core verbatim.

## Step 2: the contract

`proto/order/v1/order.proto`, with the status machine expressed as an enum
and every element commented (buf lint enforces the comments):

```proto
syntax = "proto3";

package order.v1;

import "buf/validate/validate.proto";

option go_package = "github.com/you/yourapp/services/order/internal/gen/proto/order/v1;orderv1";

// OrderService manages order intake and lifecycle.
service OrderService {
  // PlaceOrder creates an order from line items.
  rpc PlaceOrder(PlaceOrderRequest) returns (PlaceOrderResponse);
  // GetOrder returns one order.
  rpc GetOrder(GetOrderRequest) returns (GetOrderResponse);
  // AdvanceStatus moves an order one legal step forward.
  rpc AdvanceStatus(AdvanceStatusRequest) returns (AdvanceStatusResponse);
  // CancelOrder cancels while cancellation is still legal.
  rpc CancelOrder(CancelOrderRequest) returns (CancelOrderResponse);
}

// OrderStatus is the lifecycle; transitions are enforced by the domain.
enum OrderStatus {
  // ORDER_STATUS_UNSPECIFIED is the proto3 zero value, never stored.
  ORDER_STATUS_UNSPECIFIED = 0;
  // ORDER_STATUS_PENDING awaits confirmation.
  ORDER_STATUS_PENDING = 1;
  // ORDER_STATUS_CONFIRMED is accepted and queued.
  ORDER_STATUS_CONFIRMED = 2;
  // ORDER_STATUS_PREPARING is being worked on.
  ORDER_STATUS_PREPARING = 3;
  // ORDER_STATUS_COMPLETED is done; terminal.
  ORDER_STATUS_COMPLETED = 4;
  // ORDER_STATUS_CANCELLED is abandoned; terminal.
  ORDER_STATUS_CANCELLED = 5;
}

// LineItem is one priced position. Money is integer minor units (cents):
// floats lose precision and are forbidden for money everywhere in lynk.
message LineItem {
  // product_id references the catalog as an opaque id.
  string product_id = 1 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
  // quantity ordered.
  int32 quantity = 2 [(buf.validate.field).int32 = {gte: 1, lte: 1000}];
  // unit_price_minor is the price snapshot in minor units at order time.
  int64 unit_price_minor = 3 [(buf.validate.field).int64.gte = 0];
}
// ... requests/responses elided; mirror the example module's shapes.
```

Generate: `cd services/order && buf dep update && buf lint && buf generate`.

## Step 3: schema and queries

`migrations/0001_orders.up.sql`:

```sql
CREATE SCHEMA IF NOT EXISTS orders;

CREATE TABLE orders.orders (
    id           UUID PRIMARY KEY,
    customer_id  TEXT        NOT NULL,
    status       TEXT        NOT NULL,
    -- money is always integer minor units plus a currency code
    total_minor  BIGINT      NOT NULL,
    currency     TEXT        NOT NULL,
    version      BIGINT      NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

-- line items are a sub-entity: saved THROUGH the order, no own repository
CREATE TABLE orders.order_items (
    id               UUID PRIMARY KEY,
    order_id         UUID   NOT NULL REFERENCES orders.orders (id) ON DELETE CASCADE,
    product_id       TEXT   NOT NULL,
    quantity         INT    NOT NULL,
    unit_price_minor BIGINT NOT NULL
);

CREATE INDEX orders_customer_created_idx ON orders.orders (customer_id, created_at DESC);

CREATE TABLE orders.outbox (
    id           UUID PRIMARY KEY,
    subject      TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);
CREATE INDEX orders_outbox_unpublished_idx ON orders.outbox (occurred_at) WHERE published_at IS NULL;
```

Queries follow `sqlc/queries/example.sql`'s shapes: version-guarded
`UpdateOrder :execrows`, outbox claim with `FOR UPDATE SKIP LOCKED`. Create
the database and apply:

```bash
docker exec -it lynk-postgres-1 psql -U lynk -c 'CREATE DATABASE order_db'
migrate -path migrations -database 'postgres://lynk:lynk@127.0.0.1:5433/order_db?sslmode=disable' up
sqlc generate
```

## Step 4: the domain, where this service earns its keep

The Order aggregate's centerpiece is the transition map. Illegal moves are
impossible by construction, not by handler discipline:

```go
// domain/order.go
type Status string

const (
    StatusPending   Status = "pending"
    StatusConfirmed Status = "confirmed"
    StatusPreparing Status = "preparing"
    StatusCompleted Status = "completed"
    StatusCancelled Status = "cancelled"
)

// allowedTransitions IS the business rule. Changing the lifecycle means
// changing this map, nothing else.
var allowedTransitions = map[Status][]Status{
    StatusPending:   {StatusConfirmed, StatusCancelled},
    StatusConfirmed: {StatusPreparing, StatusCancelled},
    StatusPreparing: {StatusCompleted},
}

var ErrIllegalTransition = errors.New("illegal status transition")

func (o *Order) AdvanceTo(next Status, now time.Time) error {
    for _, legal := range allowedTransitions[o.status] {
        if legal == next {
            o.status = next
            o.updatedAt = now
            o.events = append(o.events, OrderStatusChanged{
                OrderID: o.id.String(), Status: string(next), OccurredAt: now,
            })
            return nil
        }
    }
    return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, o.status, next)
}
```

Two more rules the factory enforces: the total is computed from the items
(never trusted from the request), and every item carries a PRICE SNAPSHOT so
historical orders survive catalog edits. Items are a sub-entity: the
repository saves order and items in one transaction through the root.

Everything else (use cases with UnitOfWork + outbox, the repository with
optimistic locking, the handler with `classify`) mirrors the example module
file for file. `ErrIllegalTransition` classifies to
`apperror.KindFailedPrecondition`-style handling: use `KindConflict` or add
a kind if your domain needs the distinction.

## Step 5: bootstrap and event ownership

Copy `core/internal/bootstrap` and trim. The decisive lines in the worker:

```go
// THIS service owns its stream. Core's worker must NOT list order.> in
// CORE_EVENTS; one stream, one owner.
const orderStream = "ORDER_EVENTS"
var orderStreamSubjects = []string{"order.>"}

err = f.bus.EnsureStream(streamCtx, orderStream, orderStreamSubjects)
```

If the order service consumes core's events (say, it caches user display
names from `identity.user.registered`), it binds, never declares:

```go
consumer, err := f.bus.EnsureConsumer(ctx, "CORE_EVENTS", jetstream.ConsumerConfig{
    Durable:        "order-identity-projection",
    FilterSubjects: []string{"identity.user.registered"},
    AckPolicy:      jetstream.AckExplicitPolicy,
})
```

Pick the duplicate-handling strategy from the idempotency menu in
[03](03-ddd-guide.md) and write it in the consumer's comment.

## Step 6: route it at the gateway

`services/gateway/internal/edge/proxy.go`, two small changes:

```go
backends := map[string]string{
    "core":  upstreams.Core,
    "order": upstreams.Order,   // new: UPSTREAM_ORDER env, e.g. localhost:50052
}

var routes = []route{
    // ...existing...
    {"order.", "order"},
}
```

Add `Order string` to the gateway's `UpstreamsConfig` with an
`UPSTREAM_ORDER` env default, and an `.env.example` line. Clients reach the
new service through the same edge with zero client changes.

## Step 7: operations wiring

- `deploy/docker-compose.yml`: an `order` service entry (build context
  `../services/order`, port 50052, the same env anchor pattern, healthcheck
  on its ops port) and an `order-worker` entry. Add `order_db` to the
  postgres bootstrap or create it manually once.
- `.github/workflows/ci.yml`: add `order` to BOTH matrices (the `go` job and
  the `images` job).
- `.github/dependabot.yml`: a gomod entry for `/services/order`.
- Root `Makefile`: add `order` to `SERVICES`, and migrate targets if you
  want per-service shortcuts.
- Frontend (if it talks to the service): `npm run generate` picks the protos
  up once you point a second generate command at `../services/order`, or
  extend the existing script.

## Step 8: verify the slice before celebrating

```bash
cd services/order && go build ./... && go vet ./... && golangci-lint run ./...
make dev-gateway   # and run order's server + worker
grpcurl -plaintext -H "x-user-id: <uuid>" -d '{...}' localhost:50052 order.v1.OrderService/PlaceOrder
# illegal transition must fail:
grpcurl ... AdvanceStatus pending->completed   # expect FAILED/CONFLICT, not success
# event flow: the outbox row flips to published, and your consumer sees it
docker exec lynk-postgres-1 psql -U lynk -d order_db -c "SELECT subject, published_at IS NOT NULL FROM orders.outbox"
```

A service is DONE when: contract linted, schema migrated, domain rules
provably enforced (try the illegal cases), events relayed, route flipped,
compose and CI entries in place, and everything green. Anything less is a
scaffold, and scaffolds do not merge.
