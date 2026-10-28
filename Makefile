BINARY := bin/vandyke
GO ?= go
TRMNL_VERSION ?= $(shell git describe --tags --always --dirty)

.PHONY: help run build test vet fmt tidy docker compose-up compose-down clean trmnl-test trmnl-lint trmnl-package

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

run: ## Run the server locally with the default database
	$(GO) run ./cmd/vandyke

build: ## Build a static binary into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/vandyke

test: ## Run the full test suite
	$(GO) test ./...

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format all Go code
	gofmt -w .

tidy: ## Sync go.mod and go.sum
	$(GO) mod tidy

docker: ## Build the Docker image
	docker build -t vandyke .

compose-up: ## Start the stack with docker compose
	docker compose up -d --build

compose-down: ## Stop the docker compose stack
	docker compose down

clean: ## Remove build artifacts and local data
	rm -rf bin data

trmnl-test: ## Run the TRMNL transform tests
	python3 trmnl/tests/test_vandyke_transform.py

trmnl-lint: ## Lint the TRMNL plugins (requires trmnlp)
	@for d in trmnl/plugins/*/; do echo "Linting $$d"; trmnlp lint --dir "$$d"; done

trmnl-package: ## Build versioned TRMNL plugin zips into dist/
	python3 scripts/package-trmnl.py "$(TRMNL_VERSION)"
