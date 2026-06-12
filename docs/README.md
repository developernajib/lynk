# lynk documentation

Read in order the first time; afterwards each file stands alone.

| Doc                                           | What it answers                                                                           |
| --------------------------------------------- | ----------------------------------------------------------------------------------------- |
| [01 Getting started](01-getting-started.md)   | How do I go from a fresh clone to a running full stack?                                   |
| [02 Architecture](02-architecture.md)         | What are the moving parts and how does a request or event travel through them?            |
| [03 DDD guide](03-ddd-guide.md)               | How do I build and maintain features with the domain-driven structure this codebase uses? |
| [04 Modular monolith](04-modular-monolith.md) | What rules keep core's modules independent, and how does a module become a microservice?  |
| [05 Adding a service](05-adding-a-service.md) | How do I add a brand-new deployable, worked end to end with an order service?             |
| [06 Security](06-security.md)                 | What protects this system and why is each control there?                                  |
| [07 Operations](07-operations.md)             | How do I run, migrate, seed, observe, and deploy it?                                      |

Two conventions used throughout:

- Paths are written from the repository root, e.g. `services/core/internal/modules/example`.
- "Module" always means a feature package inside core's modular monolith. "Service" always means a separately deployed binary with its own go.mod.
