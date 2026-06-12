# 04 - The modular monolith and mechanical extraction

core is one deployable containing several modules. This document is the
discipline that keeps "one deployable" from becoming "one big ball", and the
procedure that turns a module into its own service when the day comes.

## Why a modular monolith at all

Microservices buy you independent scaling, independent failure, and
independent deployment, at the price of network hops, distributed
transactions, more pipelines, and more on-call surface. Most features never
need the first list and always pay the second. So lynk's default is: every
feature is a MODULE inside core, built so strictly isolated that promoting
one to a service later is mechanical rather than a rewrite.

You split a module out only when it has a distinct scaling or failure axis:
public read traffic that dwarfs everything else, a workload that must not
share a database with the rest, a component that must keep running when core
is down. "The diagram looks nicer" is not an axis.

## The module independence rules

These rules are what make extraction cheap. Break one and the future
extraction becomes a refactor; keep them and it is a copy plus a route flip.

1. **No cross-module imports. Ever.** Modules communicate through events on
   the bus, or through a primitive-typed port declared by the consumer and
   wired in bootstrap (see the ABAC checker in the example module). If you
   need another module's data synchronously, that is what its gRPC API is
   for after extraction; inside the monolith, prefer events and projections.
2. **Own schema per module.** `example.*`, `identity.*`, `authz.*`,
   `audit.*`. No cross-schema foreign keys, no cross-schema joins. A
   reference to another module's entity is a plain UUID column with no FK
   constraint: the other module might not share a database forever.
3. **Own outbox per module.** Events leave through the module's outbox table
   and relay, so after extraction the module brings its eventing with it.
4. **Own value objects, even duplicated ones.** If two modules both need an
   Email type, they each have one. Sharing "just a tiny VO" creates exactly
   the import edge rule 1 forbids. Duplication here is the cost of
   independence and it is cheap.
5. **Primitive boundaries.** Module ports and events carry strings, ints,
   maps, and times, never another module's domain types.
6. **Per-module wiring.** A module's composition root is its `module.go`.
   Bootstrap only constructs modules and hands them platform dependencies.
   No god file that knows module internals.

The bus-level rule that completes the picture: a stream has exactly ONE
owner that declares its configuration. Core's worker owns CORE_EVENTS and
the subject list lives in one place (`bootstrap/modules.go`). Consumers
everywhere use the bind-only API and can never rewrite stream config.

## How a request stays module-local

Look at how the pieces are registered in `internal/bootstrap/modules.go`:
each module gets the pools, the bus, a logger, and nothing of each other.
The gateway routes by proto package prefix (`example.` → core), so the
question "which deployable serves this API" is answered by a routing table,
not by code structure. That routing table is the extraction seam.

## Extracting a module into a service

Worked against the example module; substitute your own. Budget roughly a
day the first time, most of it verification.

**Before you start:** confirm the module actually obeys the rules above.
`grep -r "modules/example" services/core/internal/modules` must show no hits
outside the module itself; the module's queries must touch only its schema.

1. **Create the new service skeleton.** Follow
   [05 Adding a service](05-adding-a-service.md) steps 1 and 2: copy the
   platform, init the go.mod, set up cmd/server, cmd/worker, bootstrap,
   buf, sqlc, Dockerfile, env examples.
2. **Move the module's code.** Copy `internal/modules/example` into the new
   service (it can flatten to `internal/example` or keep the modules layout
   if more will follow). Copy its proto directory, its migrations, and its
   sqlc queries. Fix import paths. Nothing else comes along, which is the
   payoff of rule 1.
3. **Give it its own database.** Create `example_db`, apply the module's
   migrations there. The data migration itself (copying existing rows) is
   the one genuinely careful step: snapshot-copy the module's schema tables,
   then either freeze writes briefly during cutover or run a dual-write
   window, depending on your tolerance.
4. **Move event ownership.** The new service's worker declares its OWN
   stream (e.g. EXAMPLE_EVENTS with subjects `example.>`), and its outbox
   relay publishes there. Remove `example.>` from core's CORE_EVENTS subject
   list. Consumers that cared about these events (the audit ledger, for
   instance) add a second bind-only consumer on the new stream.
5. **Flip the route.** In `services/gateway/internal/edge/proxy.go`, add the
   new backend address to the dial map and change one line:
   `{"example.", "core"}` becomes `{"example.", "example"}`. Clients never
   notice: the proto package, methods, and wire format are unchanged.
6. **Wire operations.** Compose entry (with healthcheck), CI matrix entry,
   Dependabot entry, migrations path in the Makefile.
7. **Decommission in core.** Once the new service is verified live: remove
   the module directory, its bootstrap wiring, its gateway-internal route
   comment, and write a follow-up migration dropping its schema from core's
   database after the retention window you are comfortable with.

**What makes this mechanical:** every step above is a move or a one-line
change. There is no "untangle the shared helpers" step, because the rules
forbade shared helpers from day one. Guard that property in code review: it
is the single most valuable architectural asset in this codebase.

## Signs a module is drifting

- A PR adds an import from one module into another "just temporarily".
- A query joins across schemas because it was easy.
- A foreign key points at another module's table.
- An event grows a field that only exists so a consumer can call back.
- Two modules share a value object "to stay consistent".

Each of these is cheap to reject in review and expensive to unwind at
extraction time.
