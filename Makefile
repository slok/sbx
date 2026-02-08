.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary
	go build -o bin/sbx ./cmd/sbx
	sudo setcap cap_net_admin+ep ./bin/sbx

.PHONY: test
test: ## Run unit tests
	go test -v -race ./internal/...

.PHONY: test-integration
test-integration: ## Run integration tests
	@./scripts/check/integration-test.sh

.PHONY: test-all
test-all: test test-integration ## Run all tests

.PHONY: ci-test
ci-test: ## Run unit tests in CI environment
	@./scripts/check/unit-test.sh

.PHONY: ci-integration-test
ci-integration-test: ## Run integration tests in CI environment
	@./scripts/check/integration-test.sh

.PHONY: ci-check
ci-check: ## Run linters in CI environment
	@./scripts/check/check.sh

.PHONY: go-gen
go-gen: ## Generate mocks
	mockery

.PHONY: check
check: ## Run linters
	go vet ./...
	go fmt ./...

.PHONY: run
run: ## Run with example flags (go run)
	go run ./cmd/sbx create --name example-sandbox --engine fake --cpu 2 --mem 2048 --disk 10
