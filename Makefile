VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test lint fmt fmt-check vet check

build:
	go build -ldflags "$(LDFLAGS)" -o bin/doctolibre ./cmd/doctolibre

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/doctolibre serve

test:
	go test ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "fichiers mal formatés:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

check: fmt-check vet lint test
