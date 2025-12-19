# Copyright Contributors to the KubeTask project
SHELL := /bin/bash

all: build
.PHONY: all

# Version information
VERSION ?= latest
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Image URL to use for building/pushing image targets
IMG_REGISTRY ?= quay.io
IMG_ORG ?= kubetask
IMG_NAME ?= kubetask-controller
IMG ?= $(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME):$(VERSION)

# PLATFORMS defines the target platforms for multi-arch build
PLATFORMS ?= linux/arm64,linux/amd64

# Go packages
GO_PACKAGES := $(addsuffix ...,$(addprefix ./,$(filter-out vendor/ test/ hack/ client/,$(wildcard */))))
GO_BUILD_PACKAGES := $(GO_PACKAGES)
GO_BUILD_PACKAGES_EXPANDED := $(GO_BUILD_PACKAGES)
GO_LD_FLAGS := -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)

# Local bin directory for tools
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# controller-gen setup
CONTROLLER_GEN_VERSION := v0.16.5
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen

# golangci-lint setup
GOLANGCI_LINT_VERSION := v2.6.2
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

# Ensure GOPATH is set
check-env:
ifeq ($(GOPATH),)
	$(warning "environment variable GOPATH is empty, auto set from go env GOPATH")
export GOPATH=$(shell go env GOPATH)
endif
.PHONY: check-env

# Download controller-gen locally if not present
.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen if not present
$(CONTROLLER_GEN): $(LOCALBIN)
	@test -s $(CONTROLLER_GEN) || \
		GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

# Download golangci-lint locally if not present
.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint if not present
$(GOLANGCI_LINT): $(LOCALBIN)
	@test -s $(GOLANGCI_LINT) || \
		GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Vendor dependencies
vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor

# Update scripts
update-scripts:
	hack/update-deepcopy.sh
	hack/update-codegen.sh
.PHONY: update-scripts

# Update all generated code
update: check-env vendor update-scripts update-crds
.PHONY: update

# Generate CRDs
update-crds: controller-gen
	$(CONTROLLER_GEN) crd:crdVersions=v1 \
		paths="./api/v1alpha1" \
		output:crd:dir=./deploy/crds
	@echo "Copying CRDs to Helm chart..."
	@mkdir -p ./charts/kubetask/crds
	@cp -f ./deploy/crds/*.yaml ./charts/kubetask/crds/
	@echo "CRDs updated successfully in both locations"
.PHONY: update-crds

# Build
build:
	go build -o bin/kubetask-controller ./cmd/controller
.PHONY: build

# Test runs unit tests only.
# Integration tests are excluded via build tags (//go:build integration).
# This follows the Kubernetes ecosystem convention (kubebuilder, controller-runtime)
# where tests remain alongside code but are separated by build tags.
# See: internal/controller/suite_test.go for detailed explanation.
test:
	go test -v ./internal/...
.PHONY: test

# Integration test runs envtest-based controller tests.
# Requires -tags=integration to include files with //go:build integration.
# envtest provides a local API server and etcd for testing without a full cluster.
integration-test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test -v -tags=integration ./internal/controller/... -coverprofile cover.out
.PHONY: integration-test

# Envtest K8s version
ENVTEST_K8S_VERSION ?= 1.31.0

# envtest setup
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest if not present
$(ENVTEST): $(LOCALBIN)
	@test -s $(ENVTEST) || \
		GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Clean
clean:
	rm -rf bin/
	rm -rf vendor/
.PHONY: clean

# Verify
verify: check-env
	bash -x hack/verify-deepcopy.sh
	bash -x hack/verify-codegen.sh
.PHONY: verify

##@ Docker

# Build the docker image
docker-build:
	docker build --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_DATE) -t $(IMG) .
.PHONY: docker-build

# Push the docker image
docker-push:
	docker push $(IMG)
.PHONY: docker-push

# Build and push docker image for multiple architectures
docker-buildx:
	docker buildx create --use --name=kubetask-builder || true
	docker buildx build \
		--platform=$(PLATFORMS) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_DATE) \
		--tag $(IMG) \
		--push \
		.
.PHONY: docker-buildx

##@ KubeTask Tools Image

# Tools image settings (unified image for git-init, save-session, etc.)
TOOLS_IMG_NAME ?= kubetask-tools
TOOLS_IMG ?= $(IMG_REGISTRY)/$(IMG_ORG)/$(TOOLS_IMG_NAME):$(VERSION)

# Build kubetask-tools image
tools-build: ## Build kubetask-tools image (git-init, save-session, etc.)
	docker build -t $(TOOLS_IMG) -f cmd/tools/Dockerfile .
.PHONY: tools-build

# Push kubetask-tools image
tools-push: ## Push kubetask-tools image
	docker push $(TOOLS_IMG)
.PHONY: tools-push

# Build and push kubetask-tools image for multiple architectures
tools-buildx: ## Multi-arch build and push kubetask-tools image
	docker buildx create --use --name=kubetask-builder || true
	docker buildx build \
		--platform=$(PLATFORMS) \
		--tag $(TOOLS_IMG) \
		--push \
		-f cmd/tools/Dockerfile \
		.
.PHONY: tools-buildx

##@ Helm

# Package helm chart
helm-package:
	helm package charts/kubetask -d dist/
.PHONY: helm-package

# Install helm chart
helm-install:
	helm install kubetask charts/kubetask \
		--namespace kubetask-system \
		--create-namespace \
		--set controller.image.repository=$(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME) \
		--set controller.image.tag=$(VERSION)
.PHONY: helm-install

# Upgrade helm chart
helm-upgrade:
	helm upgrade kubetask charts/kubetask \
		--namespace kubetask-system \
		--set controller.image.repository=$(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME) \
		--set controller.image.tag=$(VERSION)
.PHONY: helm-upgrade

# Uninstall helm chart
helm-uninstall:
	helm uninstall kubetask --namespace kubetask-system
.PHONY: helm-uninstall

# Template helm chart (dry-run)
helm-template:
	helm template kubetask charts/kubetask \
		--namespace kubetask-system \
		--set controller.image.repository=$(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME) \
		--set controller.image.tag=$(VERSION)
.PHONY: helm-template

# Chart registry settings
CHART_REGISTRY ?= oci://$(IMG_REGISTRY)/$(IMG_ORG)
CHART_NAME ?= kubetask

# Login to helm registry
helm-login: ## Login to helm OCI registry
	helm registry login $(IMG_REGISTRY)
.PHONY: helm-login

# Push helm chart to OCI registry
helm-push: helm-package ## Push helm chart to OCI registry
	helm push dist/$(CHART_NAME)-*.tgz $(CHART_REGISTRY)
.PHONY: helm-push

##@ Development

# Run controller locally
run:
	go run ./cmd/controller/main.go
.PHONY: run

# Format code
fmt:
	go fmt ./...
.PHONY: fmt

# Lint code
lint: golangci-lint
	$(GOLANGCI_LINT) run
.PHONY: lint

##@ E2E Testing

# Kind cluster name for e2e testing
E2E_CLUSTER_NAME ?= kubetask-e2e
E2E_IMG_TAG ?= dev

# Create kind cluster for e2e testing
e2e-kind-create: ## Create kind cluster for e2e testing
	@if kind get clusters | grep -q "^$(E2E_CLUSTER_NAME)$$"; then \
		echo "Kind cluster '$(E2E_CLUSTER_NAME)' already exists"; \
	else \
		kind create cluster --name $(E2E_CLUSTER_NAME); \
	fi
.PHONY: e2e-kind-create

# Delete kind cluster
e2e-kind-delete: ## Delete kind cluster
	kind delete cluster --name $(E2E_CLUSTER_NAME)
.PHONY: e2e-kind-delete

# Build docker image for e2e testing
e2e-docker-build: ## Build docker image for e2e testing
	docker build --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_DATE) -t $(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME):$(E2E_IMG_TAG) .
.PHONY: e2e-docker-build

# Load docker image into kind cluster
e2e-kind-load: ## Load docker image into kind cluster
	kind load docker-image $(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME):$(E2E_IMG_TAG) --name $(E2E_CLUSTER_NAME)
.PHONY: e2e-kind-load

# Verify image in kind cluster
e2e-verify-image: ## Verify image is loaded in kind cluster
	@echo "Verifying image in kind cluster..."
	@docker exec -i $(E2E_CLUSTER_NAME)-control-plane crictl images | grep $(IMG_NAME) || \
		(echo "Error: Image not found in kind cluster" && exit 1)
	@echo "Image verified successfully"
.PHONY: e2e-verify-image

# Deploy controller to kind cluster using Helm (CRDs are in crds/ directory)
# Using uninstall + install instead of upgrade to ensure CRDs are properly installed
e2e-deploy: ## Deploy controller and CRDs to kind cluster using Helm
	@helm uninstall kubetask --namespace kubetask-system 2>/dev/null || true
	helm install kubetask charts/kubetask \
		--namespace kubetask-system \
		--create-namespace \
		--set controller.image.repository=$(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME) \
		--set controller.image.tag=$(E2E_IMG_TAG) \
		--set controller.image.pullPolicy=Never \
		--wait
.PHONY: e2e-deploy

# Undeploy controller from kind cluster (CRDs will be removed by Helm)
e2e-undeploy: ## Undeploy controller and CRDs from kind cluster
	helm uninstall kubetask --namespace kubetask-system || true
	kubectl delete namespace kubetask-system --ignore-not-found=true
.PHONY: e2e-undeploy

# Setup e2e environment (create cluster, build images, load images, and deploy)
e2e-setup: e2e-kind-create e2e-docker-build e2e-agent-build e2e-tools-build e2e-kind-load e2e-agent-load e2e-tools-load e2e-verify-image e2e-deploy ## Setup complete e2e environment
	@echo "E2E environment setup complete"
.PHONY: e2e-setup

# Teardown e2e environment (undeploy controller and delete cluster)
e2e-teardown: e2e-undeploy e2e-kind-delete ## Teardown e2e environment
	@echo "E2E environment teardown complete"
.PHONY: e2e-teardown

# Rebuild and reload controller image (for iterative development)
e2e-reload: e2e-docker-build e2e-kind-load e2e-verify-image ## Rebuild and reload controller image
	@echo "Restarting controller pods..."
	@kubectl rollout restart deployment -n kubetask-system || true
	@echo "Controller image reloaded successfully"
.PHONY: e2e-reload

# Build echo agent image for e2e testing
e2e-agent-build: ## Build echo agent image for e2e testing
	docker build -t quay.io/kubetask/kubetask-agent-echo:latest agents/echo/
.PHONY: e2e-agent-build

# Load echo agent image into kind cluster
e2e-agent-load: ## Load echo agent image into kind cluster
	kind load docker-image quay.io/kubetask/kubetask-agent-echo:latest --name $(E2E_CLUSTER_NAME)
.PHONY: e2e-agent-load

# Build kubetask-tools image for e2e testing
e2e-tools-build: ## Build kubetask-tools image for e2e testing
	docker build -t quay.io/kubetask/kubetask-tools:latest -f cmd/tools/Dockerfile .
.PHONY: e2e-tools-build

# Load kubetask-tools image into kind cluster
e2e-tools-load: ## Load kubetask-tools image into kind cluster
	kind load docker-image quay.io/kubetask/kubetask-tools:latest --name $(E2E_CLUSTER_NAME)
.PHONY: e2e-tools-load

# Run e2e tests
e2e-test: ## Run e2e tests
	@echo "Running e2e tests..."
	E2E_TEST_NAMESPACE=kubetask-e2e-test \
	E2E_ECHO_IMAGE=quay.io/kubetask/kubetask-agent-echo:latest \
	go test -v ./e2e/... -timeout 30m -ginkgo.v
.PHONY: e2e-test

# Run specific e2e test
e2e-test-focus: ## Run specific e2e test (usage: make e2e-test-focus FOCUS="Task")
	@echo "Running focused e2e tests..."
	E2E_TEST_NAMESPACE=kubetask-e2e-test \
	E2E_ECHO_IMAGE=quay.io/kubetask/kubetask-agent-echo:latest \
	go test -v ./e2e/... -timeout 30m -ginkgo.v -ginkgo.focus="$(FOCUS)"
.PHONY: e2e-test-focus

# Full e2e test workflow (setup, test, teardown)
e2e: e2e-setup e2e-test ## Run full e2e test workflow
	@echo "E2E tests complete"
.PHONY: e2e

##@ Agent

agent-base-build: ## Build universal base image
	$(MAKE) -C agents base-build

agent-base-push: ## Push universal base image
	$(MAKE) -C agents base-push

agent-base-buildx: ## Multi-arch build and push base image
	$(MAKE) -C agents base-buildx

agent-build: ## Build agent image (requires base image)
	$(MAKE) -C agents build

agent-push: ## Push agent image
	$(MAKE) -C agents push

agent-buildx: ## Multi-arch build and push agent image
	$(MAKE) -C agents buildx

agent-build-all: ## Build base and all agent images
	$(MAKE) -C agents build-all

agent-push-all: ## Push base and all agent images
	$(MAKE) -C agents push-all

agent-buildx-all: ## Multi-arch build and push all images
	$(MAKE) -C agents buildx-all

##@ Help

# Display this help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
.PHONY: help
