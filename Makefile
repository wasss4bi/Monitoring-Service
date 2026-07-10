include .env

COMPOSE ?= docker compose
MIGRATIONS_PATH ?= /app/migrations
MIGRATE_COMMAND = $(COMPOSE) exec -T monitoring_service sh -c 'migrate -path $(MIGRATIONS_PATH) -database "$$DATABASE_URL" "$$@"' --
MIGRATE_CREATE_COMMAND = $(COMPOSE) run --rm --no-deps -v "$(CURDIR):/workspace" --workdir /workspace --entrypoint migrate monitoring_service
STEPS ?= 1

upgo: docker-compose.yml
	$(COMPOSE) down monitoring_service && $(COMPOSE) up -d monitoring_service
reboot: docker-compose.yml
	$(COMPOSE) down && $(COMPOSE) up -d --build

dev: docker-compose.yml docker-compose.dev.yml .air.toml
	$(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml up --build monitoring_service

.PHONY: migrate-create migrate-up migrate-up-step migrate-down migrate-down-all migrate-goto migrate-version migrate-force

migrate-create:
	@test -n "$(NAME)" || (echo "Usage: make migrate-create NAME=add_users_email" && exit 1)
	$(MIGRATE_CREATE_COMMAND) create -ext sql -dir migrations -seq $(NAME)

migrate-up:
	$(COMPOSE) run --rm --no-deps monitoring_service go run ./cmd/migration

migrate-up-step:
	$(MIGRATE_COMMAND) up $(STEPS)

migrate-down:
	$(MIGRATE_COMMAND) down $(STEPS)

migrate-down-all:
	$(MIGRATE_COMMAND) down -all

migrate-goto:
	@test -n "$(VERSION)" || (echo "Usage: make migrate-goto VERSION=1" && exit 1)
	$(MIGRATE_COMMAND) goto $(VERSION)

migrate-version:
	$(MIGRATE_COMMAND) version

migrate-force:
	@test -n "$(VERSION)" || (echo "Usage: make migrate-force VERSION=1" && exit 1)
	$(MIGRATE_COMMAND) force $(VERSION)

.PHONY: integration-monitoring k6-create-1000-monitors k6-list-monitors

integration-monitoring:
	$(COMPOSE) up -d postgres redis rabbitmq
	$(COMPOSE) run --rm --no-deps monitoring_service go run ./cmd/migration
	INTEGRATION_DATABASE_URL="postgres://monitoring_service:monitoring_service_password@localhost:5432/monitoring_service?sslmode=disable" INTEGRATION_REDIS_URL="redis://localhost:6379/0" INTEGRATION_RABBITMQ_URL="amqp://monitoring_service:monitoring_service_password@localhost:5672/" go test -tags=integration ./integration

k6-create-1000-monitors:
	@command -v k6 >/dev/null || (echo "k6 CLI is required" && exit 1)
	k6 run k6/create_1000_monitors.js

k6-list-monitors:
	@command -v k6 >/dev/null || (echo "k6 CLI is required" && exit 1)
	k6 run k6/list_monitors.js
