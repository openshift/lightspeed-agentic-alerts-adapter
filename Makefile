BINARY_NAME := alerts-adapter
MODULE := github.com/openshift/lightspeed-agentic-alerts-adapter
CMD_DIR := ./cmd/$(BINARY_NAME)
BIN_DIR := ./bin
IMAGE_NAME ?= $(BINARY_NAME)
IMAGE_TAG ?= latest

GO := go
GOFLAGS ?=
LDFLAGS ?=
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT = $(shell which golangci-lint 2>/dev/null)

.PHONY: all build test clean lint fmt vet run coverage container-build container-push help install-lint

all: build

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)

test:
	$(GO) test $(GOFLAGS) ./...

clean:
	rm -rf $(BIN_DIR)/$(BINARY_NAME) coverage.out coverage.html

install-lint:
ifeq ($(GOLANGCI_LINT),)
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
endif

lint: install-lint
	$(GOLANGCI_LINT) run ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

run: build
	$(BIN_DIR)/$(BINARY_NAME)

coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

container-build:
	podman build --no-cache -t $(IMAGE_NAME):$(IMAGE_TAG) -f Containerfile .

container-push: container-build
	podman push $(IMAGE_NAME):$(IMAGE_TAG)

help:
	@echo "Targets:"
	@echo "  build           - Build the binary"
	@echo "  test            - Run tests"
	@echo "  clean           - Remove build artifacts"
	@echo "  lint            - Run golangci-lint"
	@echo "  fmt             - Run go fmt"
	@echo "  vet             - Run go vet"
	@echo "  run             - Build and run the binary"
	@echo "  coverage        - Generate test coverage report"
	@echo "  container-build - Build container image"
	@echo "  container-push  - Build and push container image (set IMAGE_NAME; IMAGE_TAG defaults to latest)"
	@echo "  help            - Show this help"
