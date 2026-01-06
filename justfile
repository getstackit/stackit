# Default recipe
default: check

# Setup the development environment (install tools, dependencies, and build)
setup: install-tools deps build
	@echo "✅ Setup complete! You can now run 'just check' to verify everything is working."
	@if [ ! -f ./bin/stackit ]; then \
		echo "Warning: stackit binary not found. Try running 'just build'."; \
	fi

# Download dependencies for all modules
deps:
	@echo "📦 Downloading main dependencies..."
	go mod download
	go mod tidy

# Install development tools (gotestsum, golangci-lint, goimports)
install-tools:
	@echo "🛠️ Installing development tools..."
	go install gotest.tools/gotestsum@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest

# Run all tests (with caching for faster repeated runs)
test:
	@echo "Running tests..."
	@if command -v gotestsum >/dev/null 2>&1; then \
		STACKIT_NO_LOGGING=1 gotestsum --format pkgname-and-test-fails -- ./...; \
	else \
		STACKIT_NO_LOGGING=1 go test ./...; \
	fi

# Run all tests without caching (for CI or debugging flaky tests)
test-fresh:
	@echo "Running tests (no cache)..."
	@if command -v gotestsum >/dev/null 2>&1; then \
		STACKIT_NO_LOGGING=1 gotestsum --format pkgname-and-test-fails -- ./... -count=1; \
	else \
		STACKIT_NO_LOGGING=1 go test ./... -count=1; \
	fi

# Run tests with verbose output
test-verbose:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format standard-verbose -- ./...; \
	else \
		go test -v ./...; \
	fi

# Run tests with coverage
test-coverage:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format pkgname-and-test-fails -- -coverprofile=coverage.out ./...; \
	else \
		go test -coverprofile=coverage.out ./...; \
	fi
	go tool cover -html=coverage.out -o coverage.html

# Run tests with race detection
test-race:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format pkgname-and-test-fails -- -race ./...; \
	else \
		go test -race ./...; \
	fi

# Run tests for a specific package
# Usage: just test-pkg ./testhelpers
test-pkg pkg:
	@if [ -z "{{pkg}}" ]; then \
		echo "Usage: just test-pkg ./testhelpers"; \
		exit 1; \
	fi
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format standard-verbose -- {{pkg}}; \
	else \
		go test -v {{pkg}}; \
	fi

# Run tests in watch mode (requires gotestsum)
test-watch:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --watch --format pkgname-and-test-fails -- ./...; \
	else \
		echo "gotestsum not installed. Install with: go install gotest.tools/gotestsum@latest"; \
		exit 1; \
	fi

# Run fast unit tests (excludes integration tests)
test-fast:
	#!/usr/bin/env bash
	echo "Running fast tests (excluding integration)..."
	PACKAGES=$(go list ./... | grep -v /integration)
	if command -v gotestsum &> /dev/null; then
		STACKIT_NO_LOGGING=1 gotestsum --format pkgname-and-test-fails -- $PACKAGES
	else
		STACKIT_NO_LOGGING=1 go test $PACKAGES
	fi

# Run integration tests only
test-integration:
	@echo "Running integration tests..."
	@if command -v gotestsum >/dev/null 2>&1; then \
		STACKIT_NO_LOGGING=1 gotestsum --format pkgname-and-test-fails -- ./internal/integration/... ./internal/cli/integrations/... ./internal/actions/integrations/...; \
	else \
		STACKIT_NO_LOGGING=1 go test ./internal/integration/... ./internal/cli/integrations/... ./internal/actions/integrations/...; \
	fi

# Clean test artifacts
clean:
	rm -f coverage.out coverage.html
	find . -type d -name "stackit-test-*" -exec rm -rf {} + 2>/dev/null || true

# Format code
fmt:
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	elif [ -f "$(go env GOPATH)/bin/goimports" ]; then \
		"$(go env GOPATH)/bin/goimports" -w .; \
	else \
		go fmt ./...; \
	fi

# Run linter (requires golangci-lint)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --allow-parallel-runners; \
	elif [ -f "$(go env GOPATH)/bin/golangci-lint" ]; then \
		"$(go env GOPATH)/bin/golangci-lint" run --allow-parallel-runners; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Run linter and fix issues (if supported by the linter)
lint-fix:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --allow-parallel-runners --fix; \
	elif [ -f "$(go env GOPATH)/bin/golangci-lint" ]; then \
		"$(go env GOPATH)/bin/golangci-lint" run --allow-parallel-runners --fix; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Run all checks (format, lint, fast tests)
check:
	@echo "🎨 Formatting..."
	@just fmt
	@echo "🔍 Linting..."
	@just lint
	@echo "🧪 Testing (fast)..."
	@just test-fast

# Build the stackit binary with local version info
build:
	#!/bin/bash
	mkdir -p bin
	COMMIT=$(git rev-parse --short HEAD)
	DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	go build -ldflags "-X main.version=dev-local -X main.commit=$COMMIT -X main.date=$DATE" -o bin/stackit ./cmd/stackit
	if [ ! -L ./bin/st ] && [ ! -f ./bin/st ]; then
		ln -s stackit ./bin/st
		echo "Created symlink: bin/st -> stackit"
	elif [ -L ./bin/st ]; then
		echo "Symlink bin/st already exists"
	else
		echo "Warning: bin/st already exists as a regular file, skipping symlink creation"
	fi

# Install shims to repo root for local development (. in PATH)
install-shims: build
	cp scripts/stackit-shim.sh ./stackit
	cp scripts/st-shim.sh ./st
	chmod +x ./stackit ./st
	@echo "Dev shims installed to repo root"

# Install binary to ~/.local/bin (with distinct version)
install:
	#!/bin/bash
	mkdir -p ~/.local/bin
	COMMIT=$(git rev-parse --short HEAD)
	DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	go build -ldflags "-X main.version=dev-installed -X main.commit=$COMMIT -X main.date=$DATE" -o ~/.local/bin/stackit ./cmd/stackit
	ln -sf stackit ~/.local/bin/st
	echo "Installed to ~/.local/bin"

# Remove installed binary from ~/.local/bin
uninstall:
	rm -f ~/.local/bin/stackit ~/.local/bin/st
	@echo "Removed from ~/.local/bin"

# Run stackit command (builds first, then runs)
# Usage: just run log
# Usage: just run init
run cmd:
	@echo "Building stackit..."; \
	just build
	./bin/stackit {{cmd}}

# Initialize stackit in this repository
init:
	@if [ ! -f ./bin/stackit ]; then \
		echo "Building stackit..."; \
		just build; \
	fi
	./bin/stackit init

