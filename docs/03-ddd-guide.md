# 03 - The DDD guide

How to build and maintain features the way this codebase expects. The
`example` module (notes) is the living reference: every rule below points at
code you can open.

## The two module patterns

Not every feature deserves the full structure. lynk ships both patterns and
the rule for choosing:

- **Hexagon module** (`example`, `identity`): use when the feature has real
  business rules and a lifecycle: invariants that must hold, state
  transitions that can be illegal, money, credentials. Four layers, described
  below.
- **Engine module** (`authz`, `audit`): use when the feature is a mechanism
  rather than a domain: policies-as-data, an append-only ledger, projections.
  These keep the module boundary (own schema, own wiring) but skip the
  layering: a handler or consumer over sqlc directly, documented as such in
  the package comment.

If you are unsure, ask one question: "can this feature ever be in an INVALID
state that code must prevent?" Yes means hexagon. No means engine.

## The four layers of a hexagon module

```
internal/modules/<name>/
├── domain/            pure business rules. Imports stdlib ONLY.
│   ├── vo/            value objects (validated, immutable)
│   ├── <aggregate>.go the aggregate root(s)
│   ├── events.go      facts the module announces
│   ├── errors.go      sentinel errors
│   └── repository.go  persistence PORTS (interfaces)
├── application/       use cases, one concern per file
│   ├── ports.go       everything the use cases need from outside
│   └── <verb>_<noun>.go
├── adapter/grpc/      the transport boundary
│   └── handler.go     proto ↔ domain mapping, guards, error classification
├── infrastructure/    implementations of the ports
│   ├── <x>_repository.go   sqlc-backed persistence
│   ├── outbox.go      event publisher + relay
│   └── adapters.go    everything else (hashers, caches, notifiers)
└── module.go          the module's composition root
```

The dependency rule is absolute: arrows point INWARD. Domain imports nothing
of ours. Application imports domain. Adapter and infrastructure import both.
Nothing imports adapter or infrastructure except `module.go`.

## Domain layer rules

**Aggregates have private fields.** Every mutation goes through a method
that maintains invariants. Code outside the package physically cannot
construct an invalid `Note` or set a negative version. Open
`example/domain/note.go` and notice there is no way to touch `title` except
`Update`, which also records the event.

**Factories validate; rehydration does not.** `NewNote` checks invariants
and records a creation event. `NoteFromState` does neither: stored state was
validated when written, and loading is not a business fact. Every aggregate
has this pair.

**Value objects make invalid values unrepresentable.** `vo.Email` lowercases
and parses on construction; once you hold an `Email`, it IS valid. Same for
`vo.Title`, `vo.NoteID`. New field with rules? Make a value object, validate
in its constructor, never in handlers.

**Repositories are interfaces declared in the domain.** The domain says what
persistence it needs (`NoteRepository`), infrastructure supplies it. One
repository per aggregate ROOT; sub-entities are saved through their root in
the same transaction, never given their own repository.

**Events are facts, recorded by the aggregate.** A method that changes state
appends to the aggregate's event slice; `PullEvents()` hands them over after
a successful save and clears them so each publishes exactly once. Events are
FAT: they carry the data consumers need, so consumers never call back.
Events never carry secrets: no passwords, hashes, raw tokens, or codes.

**Errors are sentinels.** `domain.ErrNoteNotFound`, `domain.ErrConcurrentUpdate`.
The domain never knows about gRPC codes; the adapter translates in exactly
one place (`classify` in the handler).

## Application layer rules

**One use case per concern.** A use case struct lists its dependencies
explicitly, takes primitives in, returns domain types out. It contains the
orchestration: validate input into value objects, load, call domain methods,
persist, publish. It contains no SQL, no proto, no HTTP.

**Ports live with their consumer.** `application/ports.go` declares exactly
the interfaces the use cases need (`Clock`, `IDGenerator`, `EventPublisher`,
`UnitOfWork`, `PasswordHasher`, ...), sized to what is used and nothing
more. Infrastructure implements them. This is what keeps use cases
deterministic: time, randomness, and ids are injected, never reached for.

**State + events commit together.** The transactional outbox is not
optional. Any use case that changes state AND announces it wraps both in
`UnitOfWork.WithinTransaction`:

```go
err = uc.uow.WithinTransaction(ctx, func(ctx context.Context) error {
    if err := uc.notes.Create(ctx, note); err != nil {
        return err
    }
    return uc.events.Publish(ctx, note.PullEvents()) // outbox row, same tx
})
```

The transaction manager is nesting-aware: a use case that calls another use
case joins the outer transaction instead of opening a second one, so
composed operations stay atomic.

**Optimistic locking is the concurrency model.** Aggregates carry a
`version`. The update SQL says `WHERE id = $1 AND version = $n`; zero
affected rows means another writer won, surfaced as `ErrConcurrentUpdate`
and mapped to a conflict status. The client re-reads and retries. Note the
detail in `UpdateNote`: the version the CALLER presented is what guards the
save, so a stale browser tab gets a conflict instead of silently
overwriting.

## Adapter layer rules

The handler does four things and nothing else: resolve the principal,
enforce access, call the use case with primitives, classify errors. Look at
`example/adapter/grpc/handler.go`:

- `requirePrincipal` is the authentication guard. Public RPCs skip it.
- The ABAC check (`AccessChecker.Allowed`) is the authorization guard,
  declared as a module-local interface with primitive types so modules never
  import each other. Bootstrap adapts the authz engine onto it.
- `classify` maps every domain sentinel to an `apperror` kind exactly once.
  The platform's error-mapping interceptor turns that into a gRPC status and
  a fingerprinted log line.

Input validation you did not write: protovalidate rules in the `.proto`
contract are enforced by a platform interceptor before the handler runs.
Put length, format, and range rules in the contract; keep semantic
validation (uniqueness, state rules) in the domain.

## Infrastructure layer rules

- Repositories call sqlc-generated methods only. Raw SQL lives in
  `sqlc/queries/*.sql`, type-checked against the migrations at generate
  time. There is no string-built SQL anywhere.
- Repositories resolve their database handle per call: the active
  transaction from context when inside a unit of work, the pool otherwise.
  That is what lets the same repository participate in any transaction
  without signature changes.
- Reads choose a pool by intent: auth and read-your-writes go to the
  primary, staleness-tolerant lists go to `pools.Read()` (replicas).
- Each module owns its outbox table and relay. The relay claims with
  `FOR UPDATE SKIP LOCKED`, publishes, and marks published after the broker
  acks: at-least-once delivery.

## Consumers and the idempotency menu

At-least-once delivery means every consumer must pick a strategy for
duplicates and state it in a comment:

| Strategy                                      | When                         | Example              |
| --------------------------------------------- | ---------------------------- | -------------------- |
| UPSERT by natural key                         | projections                  | a read-model counter |
| Producer-minted id + `ON CONFLICT DO NOTHING` | exactly-once effects (money) | usage billing        |
| Payload-hash ledger                           | side effects like email      | notification dedup   |
| Accept rare duplicates                        | append-only logs             | the audit ledger     |

The audit module is the worked consumer example: durable consumer (resumes
after restarts, fire-once across worker replicas), `Ack` on success, `Nak`
to redeliver on transient failure, `Term` on poison messages that can never
succeed, and trace continuation from the event envelope.

## Adding a feature, step by step

1. Copy `internal/modules/example` to `internal/modules/<name>`; rename the
   aggregate. Or start from empty directories following the layout above.
2. Write the contract: `proto/<name>/v1/<name>.proto` with protovalidate
   rules and doc comments (buf lint enforces them). Run `make generate`.
3. Write the migration pair in `migrations/` (own schema named after the
   module) and the queries in `sqlc/queries/<name>.sql`. Run `make generate`
   and `make migrate-up`.
4. Build inward-out: value objects, aggregate, events, errors, repository
   port, then use cases, then infrastructure, then the handler.
5. Wire it in `internal/bootstrap/modules.go`: construct in `buildModules`,
   register in `RegisterAll`, add the relay to `Runners()`, and add the
   module's subject prefix to `coreStreamSubjects`.
6. Route it at the gateway: one line in
   `services/gateway/internal/edge/proxy.go`.
7. Regenerate frontend clients: `cd frontend && npm run generate`.
8. Verify: `make build vet lint`, then exercise it live.

## What NOT to do

- Never import one module from another. Share through events, or through a
  primitive-typed port wired in bootstrap (the ABAC checker pattern).
- Never let proto, sqlc, pgx, or grpc types reach the domain layer.
- Never publish to NATS directly from a request handler. Outbox, always.
- Never edit an applied migration or generated code. New migration; regenerate.
- Never add a scaffold-only module. Finish the vertical slice: contract,
  schema, domain, use case, handler, wiring, or do not merge it.
