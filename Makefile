.PHONY: build test lint clean help release generate generate-spec generate-client

MODULE  := github.com/openshift-online/gcp-hcp-ctl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -X $(MODULE)/pkg/cli.version=$(VERSION) \
	-X $(MODULE)/pkg/cli.commit=$(COMMIT) \
	-X $(MODULE)/pkg/cli.date=$(DATE)

OAPI_CODEGEN ?= go tool oapi-codegen
TYPESPEC_DIR := api/hyperfleet/typespec

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'

generate-spec: ## Compile TypeSpec into OpenAPI spec
	cd $(TYPESPEC_DIR) && npm install --silent && ./build-schema.sh

generate-client: ## Generate Go types and client from OpenAPI spec
	$(OAPI_CODEGEN) --package=hyperfleet --generate=types -o pkg/hyperfleet/types.gen.go api/hyperfleet/openapi.yaml
	$(OAPI_CODEGEN) --package=hyperfleet --generate=client -o pkg/hyperfleet/client.gen.go api/hyperfleet/openapi.yaml
	@echo "Generated pkg/hyperfleet/{types,client}.gen.go"

generate: generate-spec generate-client ## Full pipeline: TypeSpec → OpenAPI → Go client

build: ## Build the gcphcpctl binary
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/gcphcpctl ./cmd/gcphcpctl
	@echo "Built bin/gcphcpctl"

test: ## Run unit tests
	go test -race ./...

lint: ## Run go vet
	go vet ./...

clean: ## Remove build artifacts
	rm -rf bin/

release: ## Tag and push a release (usage: make release v=0.2.0)
	@if [ -z "$(v)" ]; then echo "Usage: make release v=0.2.0"; exit 1; fi
	@echo "Tagging v$(v)..."
	git tag -a "v$(v)" -m "Release v$(v)"
	git push origin "v$(v)"
	@echo "Pushed v$(v)"
