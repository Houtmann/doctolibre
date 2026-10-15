VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test lint fmt fmt-check vet check db-up db-down db-reset migrate sqlc

build:
	go build -ldflags "$(LDFLAGS)" -o bin/doctolibre ./cmd/doctolibre

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/doctolibre serve

test:
	go test ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.claude/*')

fmt-check:
	@out="$$(gofmt -l $$(find . -name '*.go' -not -path './.claude/*'))"; if [ -n "$$out" ]; then echo "fichiers mal formatés:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

check: fmt-check vet lint test

COMPOSE := docker compose -f deploy/docker-compose.dev.yml

db-up:
	$(COMPOSE) up -d --wait postgres

db-down:
	$(COMPOSE) down

db-reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait postgres
	$(MAKE) migrate

migrate:
	go run ./cmd/doctolibre migrate up

sqlc:
	go tool sqlc generate

# Test de bout en bout des sauvegardes (nécessite Docker) : dump chiffré, restauration, rétention.
.PHONY: backup-test
backup-test:
	sh deploy/backup/test.sh
