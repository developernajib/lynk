# 06 - Security

Every control in the system, what it defends against, and where it lives.
The guiding rules: validate at the boundary, deny by default, verify
cryptographically once and propagate trust explicitly, and never let a
security knob exist without an implementation behind it.

## The edge chain (gateway)

Every public request passes this middleware chain in this exact order; the
order is part of the design (cheap rejects before expensive work, rate
limits before crypto so floods never get free CPU):

| Stage                                     | Defends against                                                                                     |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Recovery                                  | a panic taking the gateway down                                                                     |
| Request ID                                | unattributable incidents (correlates every log and trace)                                           |
| Logging (exceptions only)                 | log flooding at high volume; 5xx and slow requests still always logged                              |
| HTTPS redirect (prod)                     | SSL stripping on the first visit, before HSTS applies                                               |
| Security headers                          | clickjacking (frame DENY), MIME sniffing, referrer leaks, feature abuse; HSTS pins HTTPS for a year |
| CSP with per-request nonce                | XSS: injected scripts cannot know the nonce, so the browser refuses them                            |
| Gzip                                      | nothing (performance), but it deliberately skips gRPC-Web framing                                   |
| CORS exact-match allowlist                | hostile origins making credentialed calls; never `*`, response varies on Origin                     |
| Body limit (10 MB default)                | memory-exhaustion uploads                                                                           |
| Timeout                                   | slowloris-style connection pinning                                                                  |
| Global rate limit (in-memory)             | total overload even if Redis is down                                                                |
| Per-IP rate limit (Redis, sliding window) | bots, scrapers, single-source floods                                                                |
| CSRF double-submit cookie                 | forged cookie-bearing requests; constant-time compare, SameSite=Strict, rotated per response        |
| Authentication                            | forged identities (below)                                                                           |
| Per-endpoint rate limit                   | credential stuffing: Login at 10/min/IP makes a million guesses take ~70 days                       |

The HTTP server itself ships with read, write, idle, and header timeouts ON.
A server without them is a denial-of-service target regardless of what runs
behind it.

The sliding-window limiter is a two-key weighted counter in one atomic Lua
script: unlike a fixed window it cannot be gamed with a double burst at the
boundary. The Redis-backed levels fail OPEN (an outage degrades enforcement
rather than taking the API down) while the in-memory global level keeps the
hard ceiling; alert on Redis downtime.

## Identity and tokens

- **Passwords** are argon2id (OWASP first choice, memory-hard) in PHC
  format; legacy bcrypt hashes still verify so imported user tables work.
  Hash parameters live in the hash string, so they can be raised later
  without breaking stored credentials.
- **Access tokens** are short-lived RS256 JWTs. Asymmetric on purpose: only
  core holds the private key and can SIGN; the gateway holds only the public
  key and can VERIFY. The verifier pins the algorithm to RS256 (rejecting
  the classic alg=none and HMAC-confusion attacks) and one shared config
  feeds signer and verifier so the issuer claim can never drift.
- **Refresh tokens** are opaque 256-bit random values, stored only as
  SHA-256, and ROTATE on every use: whoever presents a refresh token second
  (victim or thief) gets thrown out. Rotation revokes the predecessor in the
  same transaction that issues the successor.
- **Logout** revokes the refresh token and blacklists the access token's
  jti in Redis until its natural expiry. Gateways keep an in-memory Bloom
  filter in front of that blacklist (a "definitely not revoked" answer costs
  ~100ns, no network), fed in real time over Redis pub/sub.
- **Account lockout**: 5 failed logins in 15 minutes locks the identifier
  for the window. Keyed by the SUBMITTED email (hashed), so unknown accounts
  throttle identically to real ones and lockout behavior cannot be used to
  enumerate users. Unknown email and wrong password return the same error
  through the same code path for the same reason. The throttle fails OPEN:
  Redis being down must not lock every user out.
- **OTP codes** (password reset, email verification) are 6 uniformly random
  digits, stored hashed, single-use, 15-minute expiry, compared in constant
  time, delivered through a Notifier port (the dev stub logs them loudly).
  Reset responses never reveal account existence.
- **API keys** are `lynk_`-prefixed 256-bit secrets shown exactly once and
  stored as SHA-256. Validation caches positives in Redis for 10 minutes
  with a reverse index, so revocation takes effect immediately rather than
  at TTL expiry.

## The trust boundary between services

The gateway verifies a JWT once, then injects `x-user-id`, `x-role`, and
`x-token-type` headers downstream. Three things make that safe:

1. The gateway STRIPS those headers from every inbound request first, so a
   client can never inject an identity.
2. The transparent proxy forwards ONLY an allowlist of metadata.
3. In production, gateway-to-backend connections use mutual TLS 1.3: the
   backend requires a client certificate from the internal CA
   (`GRPC_TLS_CLIENT_CA_FILE`), so only the gateway can speak to it, and the
   gateway verifies the backend's certificate in return. A partially
   configured cert trio fails startup instead of silently downgrading.

Inside core, interceptors rebuild the Principal from those headers. Streams
get their own interceptor because gRPC streams bypass unary chains.

## Authorization: ABAC

Authorization is attribute-based, evaluated by the authz module's engine:

- Policies are rows: effect (allow or deny), resource type, action, and a
  CEL expression over `subject`, `resource`, `action`, `resource_type`.
- Decisions are deny-by-default; any matching deny overrides every allow; an
  expression evaluation error counts as no-match, which is the deny-safe
  direction.
- Policies are editable at runtime through the admin API, which validates
  that the expression compiles before saving. The engine caches compiled
  policies in-process and refreshes asynchronously, so decisions never block
  on the database.
- Two guards are deliberately CODE, not policy: policy CRUD itself and role
  assignment require the admin role, so a broken policy set can never lock
  administrators out of repairing it.

Modules enforce decisions in their handlers through a primitive-typed port
(see `example`'s UpdateNote): owner-scoped queries are the first line,
policies the second.

## Input handling

- protovalidate rules in the proto contracts are enforced by an interceptor
  before any handler runs: lengths, formats, ranges, UUID shapes.
- All SQL is sqlc-generated with parameters. String-built SQL does not exist
  in the codebase; keep it that way.
- Value objects re-validate at the domain boundary (defense in depth), and
  application-level clamps back the proto rules.

## Secrets and cryptography

- Every secret, token, code, nonce, and ID comes from crypto/rand through
  the `secure` package. math/rand and time-seeded generation are banned for
  anything security-relevant.
- Nothing secret is ever stored retrievably: passwords are KDF-hashed,
  refresh tokens, API keys, and OTP codes are SHA-256 digests of
  high-entropy values.
- Events and logs never carry credentials. The error mapper returns generic
  public messages and keeps detail in structured logs.
- Configuration secrets arrive through the environment. `.env` is
  git-ignored; `.env.example` documents every variable.

## Supply chain

- gitleaks runs in pre-commit and CI (full history) to stop committed
  credentials.
- govulncheck runs per service in CI against the Go vulnerability database.
- Trivy scans the built container images in CI, failing on HIGH and
  CRITICAL fixable findings.
- Dependabot raises weekly grouped PRs per ecosystem (Go per service, npm,
  Docker base images, GitHub Actions). Dependencies are installed at latest
  and pinned by lockfiles (go.sum, package-lock.json).
- Images are distroless and run as nonroot: no shell, no package manager,
  minimal attack surface. The healthcheck is a static probe binary for that
  reason.

## Auditability

The audit module subscribes to every core event subject and writes an
append-only ledger, so security-relevant facts (registrations, password and
role changes, API key lifecycle) are recorded without any module having to
remember to audit. Admins read it through `audit.v1.AuditService/ListAuditLog`.

## What is intentionally NOT here (yet)

Honest gaps to know about before production:

- Email delivery is a logging stub; wire a real provider behind the
  Notifier port before exposing password reset publicly.
- The gateway does not authenticate API keys at the edge; services validate
  them via `ValidateAPIKey`. Add an edge check if machine clients should
  terminate at the gateway.
- There is no WAF, no IP reputation, and no bot scoring; put a CDN/WAF in
  front for internet-scale exposure.
- Refresh tokens live in the SPA's memory (and logout clears them); if you
  need refresh across page reloads, prefer a gateway-set httpOnly cookie
  over localStorage.
