# Root Makefile: every routine command in one place. `make help` lists them.
# Services are independent Go modules; loops run the same target in each.

SERVICES := core gateway
MIGRATE  := migrate
DB_URL   ?= postgres://lynk:lynk@127.0.0.1:5433/core_db?sslmode=disable

.PHONY: help build vet lint fmt generate \
        dev-core dev-core-worker dev-gateway \
        migrate-up migrate-down migrate-status migrate-create db-fresh db-seed \
        infra-up infra-down stack-up stack-down hooks

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: ## Build every service
	@for s in $(SERVICES); do echo "== build $$s"; (cd services/$$s && go build ./...) || exit 1; done

vet: ## Vet every service
	@for s in $(SERVICES); do echo "== vet $$s"; (cd services/$$s && go vet ./...) || exit 1; done

lint: ## Lint every service (golangci-lint v2)
	@for s in $(SERVICES); do echo "== lint $$s"; (cd services/$$s && golangci-lint run ./...) || exit 1; done

fmt: ## gofmt every service
	@for s in $(SERVICES); do (cd services/$$s && gofmt -w .); done

generate: ## Regenerate protobuf + sqlc code (core)
	cd services/core && buf generate && sqlc generate

# Hot reload (one terminal each; install: go install github.com/air-verse/air@latest)
dev-core: ## Hot-reload the core API server
	cd services/core && air

dev-core-worker: ## Hot-reload the core worker
	cd services/core && air -c .air.worker.toml

dev-gateway: ## Hot-reload the gateway
	cd services/gateway && air

# Database ergonomics (golang-migrate; install with:
#   go install -tags 'postgres,file' github.com/golang-migrate/migrate/v4/cmd/migrate@latest)
migrate-up: ## Apply pending core migrations
	$(MIGRATE) -path services/core/migrations -database "$(DB_URL)" up

migrate-down: ## Roll back ONE core migration
	$(MIGRATE) -path services/core/migrations -database "$(DB_URL)" down 1

migrate-status: ## Show current core migration version
	$(MIGRATE) -path services/core/migrations -database "$(DB_URL)" version

migrate-create: ## Create a migration pair: make migrate-create NAME=add_x
	$(MIGRATE) create -ext sql -dir services/core/migrations -seq $(NAME)

db-fresh: ## Drop everything and re-apply all migrations (DESTRUCTIVE, dev only)
	$(MIGRATE) -path services/core/migrations -database "$(DB_URL)" drop -f
	$(MIGRATE) -path services/core/migrations -database "$(DB_URL)" up

db-seed: ## Create the development admin account (idempotent)
	cd services/core && go run ./cmd/seed

infra-up: ## Start postgres + redis + nats only
	cd deploy && docker compose up -d postgres redis nats

infra-down: ## Stop the infrastructure containers
	cd deploy && docker compose stop postgres redis nats

stack-up: ## Build and start the full stack
	cd deploy && docker compose up -d --build

stack-down: ## Stop the full stack
	cd deploy && docker compose down

hooks: ## Install the pre-commit hooks
	pre-commit install
