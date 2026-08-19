BINARY_NAME := alerts-adapter
MODULE := github.com/openshift/lightspeed-agentic-alerts-adapter
CMD_DIR := ./cmd/$(BINARY_NAME)
BIN_DIR := ./bin
IMAGE_NAME ?= $(BINARY_NAME)
IMAGE_TAG ?= latest

E2E_NAMESPACE ?= openshift-lightspeed
E2E_DEPLOYMENT ?= lightspeed-agentic-alerts-adapter

GO := go
GOFLAGS ?=
LDFLAGS ?=
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT = $(shell which golangci-lint 2>/dev/null)
GINKGO = $(shell which ginkgo 2>/dev/null)

E2E_INTERNAL_REGISTRY = image-registry.openshift-image-registry.svc:5000
E2E_IMAGE_REF = $(E2E_NAMESPACE)/$(BINARY_NAME):e2e

.PHONY: all build test clean lint fmt vet run coverage container-build container-push help install-lint deploy-e2e deploy-e2e-local test-e2e undeploy-e2e install-ginkgo

all: build

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)

test:
	$(GO) test $(GOFLAGS) $$($(GO) list ./... | grep -v /test/e2e)

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

install-ginkgo:
ifeq ($(GINKGO),)
	$(GO) install github.com/onsi/ginkgo/v2/ginkgo@latest
	$(eval GINKGO=$(shell go env GOPATH)/bin/ginkgo)
endif

deploy-e2e-local:
	@hack/deploy-e2e-local.sh $(E2E_IMAGE_REF) $(E2E_INTERNAL_REGISTRY)

deploy-e2e:
	hack/deploy-e2e.sh

test-e2e: install-ginkgo
	$(GINKGO) -v --timeout=30m ./test/e2e/...

undeploy-e2e:
	oc delete -f manifests/ --ignore-not-found -n $(E2E_NAMESPACE)

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
	@echo "  deploy-e2e-local - Build, push to cluster registry, and deploy for E2E testing"
	@echo "  deploy-e2e      - Deploy adapter to cluster for E2E testing (requires IMAGE env var)"
	@echo "  test-e2e        - Run E2E test suite (requires deployed adapter)"
	@echo "  undeploy-e2e    - Remove adapter resources from cluster"
	@echo "  help            - Show this help"
