# Makefile for building and managing the Go application
# Set LDFlags for versioning and build information
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
APP_NAME := go-api-template
LDFLAGS := -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.appName=$(APP_NAME)

# Set FORCE=1 to overwrite existing signing keys with `make keys`.
FORCE ?= 0

.PHONY: all build run test check fmt lint vulncheck swagger clean clean-swagger keys clean-keys

all: check test build

## build: compile the application into build/main (never touches keys or docs)
build:
	go build -ldflags "$(LDFLAGS)" -o build/main ./cmd/api

## run: build and run the application
run: build
	./build/main

## test: run the test suite with the race detector
test:
	go test -race -count=1 ./...

## check: non-mutating checks (formatting, vet, module tidiness)
check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go mod tidy -diff

## fmt: format the code and tidy modules (mutating)
fmt:
	gofmt -w .
	go mod tidy

## lint: run golangci-lint if installed
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping (https://golangci-lint.run/welcome/install/)"; \
	fi

## vulncheck: scan dependencies for known vulnerabilities
vulncheck:
	govulncheck ./...

## swagger: regenerate the OpenAPI docs in docs/
swagger:
	swag init -g cmd/api/main.go -o docs

## clean: remove build artifacts
clean:
	rm -rf build

## clean-swagger: remove generated OpenAPI docs
clean-swagger:
	rm -rf docs

## keys: generate the RSA key pair for JWT signing if missing (FORCE=1 to overwrite)
keys:
	@if [ -f keys/private.pem ] && [ -f keys/public.pem ] && [ "$(FORCE)" != "1" ]; then \
		echo "Keys already exist in keys/ (use FORCE=1 to regenerate)"; \
	else \
		mkdir -p keys && \
		echo "Generating RSA private key..." && \
		openssl genrsa -out keys/private.pem 2048 && \
		echo "Generating RSA public key..." && \
		openssl rsa -in keys/private.pem -pubout -out keys/public.pem && \
		echo "Keys generated successfully in keys/ directory"; \
	fi

## clean-keys: delete the JWT signing keys
clean-keys:
	rm -rf keys
