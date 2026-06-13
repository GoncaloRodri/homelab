# Usage: include infrastructure/Makefile/service.mk
# SERVICE_NAME is inferred from the directory name by default.
# Optional: IMAGE_TAG, K8S_DIR, CLUSTER_NAME
# Auto-detects Go project (main/ dir) vs Node project (package.json)

SERVICE_NAME  ?= $(notdir $(CURDIR))
SERVICE_DIR   ?= .
K8S_DIR       ?= $(SERVICE_DIR)/k8s
PROJECT_ROOT  ?= $(shell git rev-parse --show-toplevel 2>/dev/null || \
	dir=$(CURDIR); until [ -f "$$dir/go.mod" ] || [ "$$dir" = / ]; do dir=$$(dirname "$$dir"); done; \
	[ -f "$$dir/go.mod" ] && echo "$$dir" || echo "")

_abs_root     := $(abspath $(PROJECT_ROOT))
_rel_path     := $(patsubst $(_abs_root)/%,%,$(CURDIR))
NAMESPACE     ?= $(word 2,$(subst /, ,$(_rel_path)))

IMAGE_TAG     ?= latest
IMAGE         ?= homelab/$(SERVICE_NAME):$(IMAGE_TAG)
CLUSTER_NAME  ?= homelab

_is_node       := $(shell [ -f $(SERVICE_DIR)/package.json ] && echo yes)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the Docker image
	docker build -t $(IMAGE) -f $(SERVICE_DIR)/Dockerfile $(PROJECT_ROOT)

.PHONY: load
load: ## Load the image into the k3d cluster
	k3d image import $(IMAGE) -c $(CLUSTER_NAME)

.PHONY: deploy
deploy: ## Apply k8s manifests to the cluster
	@kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f $(K8S_DIR)/ -n $(NAMESPACE)

.PHONY: build-deploy
build-deploy: build load deploy ## Build, load, and deploy in one step

.PHONY: skaffold-gen
skaffold-gen: ## Generate skaffold.yaml for the service
	@echo "Generating $(SERVICE_DIR)/skaffold.yaml ..."
	@ROOT="$$(cd "$(PROJECT_ROOT)" && pwd)"; \
	SVC="$$(pwd)"; \
	REL="$${SVC#$$ROOT/}"; \
	printf 'apiVersion: skaffold/v4beta13\nkind: Config\nmetadata:\n  name: %s\nbuild:\n  artifacts:\n    - image: homelab/%s\n      context: %s\n      docker:\n        dockerfile: %s/Dockerfile\n  local:\n    push: false\nmanifests:\n  rawYaml:\n    - k8s/*.yaml\ndeploy:\n  kubectl: {}\n' "$(SERVICE_NAME)" "$(SERVICE_NAME)" "$(PROJECT_ROOT)" "$$REL" > $(SERVICE_DIR)/skaffold.yaml

.PHONY: skaffold-dev
skaffold-dev: ## Run skaffold in dev mode (auto-sync, port-forward)
	skaffold dev -f $(SERVICE_DIR)/skaffold.yaml

.PHONY: skaffold-run
skaffold-run: ## Run skaffold once (build + deploy)
	skaffold run -f $(SERVICE_DIR)/skaffold.yaml

.PHONY: skaffold-delete
skaffold-delete: ## Delete resources deployed by skaffold
	skaffold delete -f $(SERVICE_DIR)/skaffold.yaml
