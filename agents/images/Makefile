# Copyright Contributors to the KubeTask project
SHELL := /bin/bash

# Agent to build (gemini, claude, codex, kimi, etc.)
AGENT ?= gemini

# Image URL configuration
IMG_REGISTRY ?= quay.io
IMG_ORG ?= zhaoxue
IMG_NAME ?= kubetask-agent-$(AGENT)
VERSION ?= latest
IMG ?= $(IMG_REGISTRY)/$(IMG_ORG)/$(IMG_NAME):$(VERSION)

# PLATFORMS defines the target platforms for multi-arch build
PLATFORMS ?= linux/arm64,linux/amd64

.PHONY: all
all: build

##@ Build

.PHONY: build
build: ## Build agent docker image (AGENT=gemini|claude|codex|kimi)
	@if [ ! -d "$(AGENT)" ]; then \
		echo "Error: Agent '$(AGENT)' not found. Available agents:"; \
		ls -d */ 2>/dev/null | sed 's/\///g'; \
		exit 1; \
	fi
	docker build -t $(IMG) $(AGENT)/

.PHONY: push
push: ## Push the docker image
	docker push $(IMG)

.PHONY: buildx
buildx: ## Build and push docker image for multiple architectures
	@if [ ! -d "$(AGENT)" ]; then \
		echo "Error: Agent '$(AGENT)' not found. Available agents:"; \
		ls -d */ 2>/dev/null | sed 's/\///g'; \
		exit 1; \
	fi
	docker buildx create --use --name=kubetask-agent-builder || true
	docker buildx build \
		--platform=$(PLATFORMS) \
		--tag $(IMG) \
		--push \
		$(AGENT)/

.PHONY: list
list: ## List available agents
	@echo "Available agents:"
	@ls -d */ 2>/dev/null | sed 's/\///g'

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""
	@echo "Examples:"
	@echo "  make build                    # Build gemini (default)"
	@echo "  make AGENT=claude build       # Build claude"
	@echo "  make AGENT=codex buildx       # Multi-arch build codex"
	@echo "  make list                     # List available agents"
