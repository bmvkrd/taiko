# Project variables
PROJECT_NAME := taiko
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go variables
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOFLAGS := -v
LDFLAGS := -ldflags="-w -s -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

# Docker variables
DOCKER_REGISTRY ?= ghcr.io/bmvkrd
DOCKER_IMAGE := $(DOCKER_REGISTRY)/$(PROJECT_NAME)
DOCKER_TAG ?= $(VERSION)

# KIND variables
KIND_CLUSTER := taiko

# Build directories
BIN_DIR := bin

.PHONY: all
all: build

## Build

.PHONY: build
build: ## Build the binary
	$(GOBUILD) $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(PROJECT_NAME) ./cmd/main.go

.PHONY: build-linux
build-linux: ## Build for Linux
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(PROJECT_NAME)-linux-amd64 ./cmd/main.go

## Development

.PHONY: fmt
fmt: ## Format code
	$(GOFMT) -s -w .

.PHONY: lint
lint: ## Run linter
	golangci-lint run ./...

.PHONY: tidy
tidy: ## Tidy go modules
	$(GOMOD) tidy

## Testing

.PHONY: test
test: ## Run tests
	$(GOTEST) -v -race -cover ./...

## Docker

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		.

.PHONY: docker-push
docker-push: ## Push Docker image
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

.PHONY: docker-kind-load
docker-kind-load: ## Upload the docker image to Kind cluster
	kind load docker-image $(DOCKER_IMAGE):$(DOCKER_TAG) --name ${KIND_CLUSTER}

## Cleanup

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BIN_DIR)

## Help

.PHONY: help
help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
