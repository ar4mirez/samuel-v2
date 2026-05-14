# Samuel v2 Makefile

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -ldflags "-s -w \
	-X github.com/ar4mirez/samuel/internal/commands.Version=$(VERSION) \
	-X github.com/ar4mirez/samuel/internal/commands.Commit=$(COMMIT) \
	-X github.com/ar4mirez/samuel/internal/commands.BuildDate=$(BUILD_DATE)"

GOCMD   := go
GOBUILD := $(GOCMD) build
GOTEST  := $(GOCMD) test
GOMOD   := $(GOCMD) mod
GOFMT   := gofmt
GOLINT  := golangci-lint

BINARY_NAME := samuel
BINARY_PATH := ./bin/$(BINARY_NAME)
MAIN_PACKAGE := ./cmd/samuel

.PHONY: all build build-all clean test test-coverage lint fmt deps install uninstall run version release release-dry help wasm-fixtures wasm-bench

all: deps lint test build

## Build the binary
build:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	@mkdir -p ./bin
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) $(MAIN_PACKAGE)
	@echo "Built: $(BINARY_PATH)"

## Build for all release platforms (Linux + macOS, amd64 + arm64)
build-all:
	@echo "Building for all platforms..."
	@mkdir -p ./bin
	GOOS=darwin  GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o ./bin/$(BINARY_NAME)-darwin-amd64  $(MAIN_PACKAGE)
	GOOS=darwin  GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o ./bin/$(BINARY_NAME)-darwin-arm64  $(MAIN_PACKAGE)
	GOOS=linux   GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o ./bin/$(BINARY_NAME)-linux-amd64   $(MAIN_PACKAGE)
	GOOS=linux   GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o ./bin/$(BINARY_NAME)-linux-arm64   $(MAIN_PACKAGE)
	@echo "Built all platforms in ./bin/"

## Install locally
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@cp $(BINARY_PATH) /usr/local/bin/$(BINARY_NAME)
	@echo "Installed. Run 'samuel version' to verify."

## Uninstall
uninstall:
	@echo "Removing $(BINARY_NAME) from /usr/local/bin..."
	@rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstalled."

## Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf ./bin ./site ./dist coverage.out coverage.html
	@$(GOCMD) clean
	@echo "Clean complete."

## Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -race -cover ./...

## Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Run linter
lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) >/dev/null 2>&1; then \
		$(GOLINT) run ./...; \
	else \
		echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -w -s .
	@echo "Format complete."

## Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Dependencies ready."

## Run the CLI (dev mode); use ARGS="..." to pass arguments
run:
	@$(GOCMD) run $(MAIN_PACKAGE) $(ARGS)

## Show version info
version:
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

## Test goreleaser locally
release-dry:
	@echo "Running goreleaser (snapshot)..."
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean; \
	else \
		echo "goreleaser not installed. Run: go install github.com/goreleaser/goreleaser/v2@latest"; \
	fi

## Release with goreleaser (CI only; expects a tag)
release:
	@echo "Running goreleaser..."
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --clean; \
	else \
		echo "goreleaser not installed."; \
	fi

## Rebuild the committed wasm e2e fixture binary
wasm-fixtures:
	@echo "Rebuilding testdata/wasm-fixture/plugin.wasm..."
	$(GOCMD) run ./scripts/wasm-fixtures -out testdata/wasm-fixture/plugin.wasm
	@echo "Rebuilt. Commit the new bytes if intentional."

## Run wasm cold-start + warm-invoke benchmarks (PRD 0009 §Functional 3)
wasm-bench:
	@echo "Running wasm cold-start + warm benchmarks..."
	$(GOTEST) -run=^$$ -bench=BenchmarkColdStart -bench=BenchmarkWarmInvoke \
		-benchtime=10x -count=5 ./internal/plugin/wasm/

## Help
help:
	@echo "Samuel v2 Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all            deps + lint + test + build (default)"
	@echo "  build          build ./bin/samuel"
	@echo "  build-all      cross-compile for Linux + macOS"
	@echo "  install        copy ./bin/samuel to /usr/local/bin"
	@echo "  uninstall      remove from /usr/local/bin"
	@echo "  test           go test -race -cover ./..."
	@echo "  test-coverage  generate coverage.html"
	@echo "  lint           golangci-lint"
	@echo "  fmt            gofmt -w -s ."
	@echo "  deps           go mod download + tidy"
	@echo "  run            go run ./cmd/samuel (use ARGS='...')"
	@echo "  release-dry    goreleaser snapshot"
	@echo "  release        goreleaser release (CI)"
	@echo "  clean          remove ./bin ./dist coverage artifacts"
