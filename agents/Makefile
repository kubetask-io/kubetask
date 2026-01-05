# Copyright Contributors to the KubeOpenCode project
SHELL := /bin/bash

# Agent to build (opencode, devbox, code-server, etc.)
AGENT ?= devbox

# No-cache option for fresh builds (usage: make NO_CACHE=1 build)
# This is useful when agent Dockerfiles use dynamic installation methods
# (e.g., curl | bash, npm install without version) that don't trigger cache
# invalidation when upstream packages are updated.
NO_CACHE ?=
NO_CACHE_FLAG := $(if $(NO_CACHE),--no-cache,)

# Image URL configuration
IMG_REGISTRY ?= quay.io
IMG_ORG ?= kubeopencode
IMG_NAME ?= kubeopencode-agent-$(AGENT)
VERSION ?= latest
IMG ?= $(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME):$(VERSION)

# Base image configuration
BASE_IMG_NAME ?= kubeopencode-agent-base
BASE_IMG ?= $(IMG_REGISTRY)/$(IMG_ORG)/$(BASE_IMG_NAME):$(VERSION)

# PLATFORMS defines the target platforms for multi-arch build
PLATFORMS ?= linux/arm64,linux/amd64

.PHONY: all
all: build

##@ Base Image

.PHONY: base-build
base-build: ## Build the universal base image
	docker build $(NO_CACHE_FLAG) -t $(BASE_IMG) base/

.PHONY: base-push
base-push: ## Push the base image to registry
	docker push $(BASE_IMG)

.PHONY: base-buildx
base-buildx: ## Build and push base image for multiple architectures
	@docker buildx use kubeopencode-builder 2>/dev/null || docker buildx create --use --name=kubeopencode-builder
	docker buildx build \
		$(NO_CACHE_FLAG) \
		--platform=$(PLATFORMS) \
		--tag $(BASE_IMG) \
		--push \
		base/

##@ Agent Images

.PHONY: build
build: ## Build agent docker image (AGENT=opencode|devbox|code-server)
	@if [ ! -d "$(AGENT)" ]; then \
		echo "Error: Agent '$(AGENT)' not found. Available agents:"; \
		ls -d */ 2>/dev/null | sed 's/\///g' | grep -v '^base$$'; \
		exit 1; \
	fi
	@if [ "$(AGENT)" = "base" ]; then \
		echo "Use 'make base-build' to build the base image"; \
		exit 1; \
	fi
	docker build $(NO_CACHE_FLAG) --build-arg BASE_IMAGE=$(BASE_IMG) -t $(IMG) $(AGENT)/

.PHONY: push
push: ## Push the agent docker image
	docker push $(IMG)

.PHONY: buildx
buildx: ## Build and push agent image for multiple architectures
	@if [ ! -d "$(AGENT)" ]; then \
		echo "Error: Agent '$(AGENT)' not found. Available agents:"; \
		ls -d */ 2>/dev/null | sed 's/\///g' | grep -v '^base$$'; \
		exit 1; \
	fi
	@if [ "$(AGENT)" = "base" ]; then \
		echo "Use 'make base-buildx' to build the base image"; \
		exit 1; \
	fi
	@docker buildx use kubeopencode-builder 2>/dev/null || docker buildx create --use --name=kubeopencode-builder
	docker buildx build \
		$(NO_CACHE_FLAG) \
		--build-arg BASE_IMAGE=$(BASE_IMG) \
		--platform=$(PLATFORMS) \
		--tag $(IMG) \
		--push \
		$(AGENT)/

##@ Build All

.PHONY: build-all
build-all: base-build ## Build base and all agent images
	@for agent in $$(ls -d */ 2>/dev/null | sed 's/\///g' | grep -v '^base$$'); do \
		echo "Building agent: $$agent"; \
		$(MAKE) AGENT=$$agent NO_CACHE=$(NO_CACHE) build; \
	done

.PHONY: push-all
push-all: base-push ## Push base and all agent images
	@for agent in $$(ls -d */ 2>/dev/null | sed 's/\///g' | grep -v '^base$$'); do \
		echo "Pushing agent: $$agent"; \
		$(MAKE) AGENT=$$agent push; \
	done

.PHONY: buildx-all
buildx-all: base-buildx ## Build and push base and all agent images (multi-arch)
	@for agent in $$(ls -d */ 2>/dev/null | sed 's/\///g' | grep -v '^base$$'); do \
		echo "Building agent (multi-arch): $$agent"; \
		$(MAKE) AGENT=$$agent NO_CACHE=$(NO_CACHE) buildx; \
	done

##@ Utilities

.PHONY: list
list: ## List available agents
	@echo "Base image:"
	@echo "  base"
	@echo ""
	@echo "Agent images:"
	@ls -d */ 2>/dev/null | sed 's/\///g' | grep -v '^base$$' | sed 's/^/  /'

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""
	@echo "Examples:"
	@echo "  make base-build               # Build the universal base image"
	@echo "  make build                    # Build devbox (default) using base"
	@echo "  make AGENT=opencode build     # Build opencode using base"
	@echo "  make NO_CACHE=1 build         # Build without cache (fetch latest)"
	@echo "  make NO_CACHE=1 build-all     # Rebuild all agents without cache"
	@echo "  make build-all                # Build base + all agents"
	@echo "  make buildx-all               # Multi-arch build base + all agents"
	@echo "  make list                     # List available agents"
