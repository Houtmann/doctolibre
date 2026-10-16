VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test lint fmt fmt-check vet check db-up db-down db-reset migrate sqlc tools css css-check

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

check: fmt-check vet lint test css-check

# Outillage front : exécutable autonome Tailwind v4 (sans Node), version et sha256 épinglés (tools/).
tools:
	sh tools/fetch-tailwind.sh

# Génère internal/web/static/ui.css (commité) à partir d'input.css et des templates.
css:
	bin/tailwindcss -i internal/web/input.css -o internal/web/static/ui.css --minify

# ui.css doit être à jour. Sauté (avec message) si bin/tailwindcss est absent, sauf en CI (CI=true).
css-check:
	@if [ ! -x bin/tailwindcss ]; then \
	  if [ -n "$$CI" ]; then echo "css-check: bin/tailwindcss absent (make tools)"; exit 1; fi; \
	  echo "css-check sauté : bin/tailwindcss absent (make tools)"; \
	else \
	  tmp=$$(mktemp) && bin/tailwindcss -i internal/web/input.css -o "$$tmp" --minify >/dev/null 2>&1 && cmp -s "$$tmp" internal/web/static/ui.css && rm -f "$$tmp" || { rm -f "$$tmp"; echo "css-check : ui.css n'est pas à jour, lancer make css"; exit 1; }; \
	fi

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
