# Usage: include infrastructure/Makefile/service.mk
# SERVICE_NAME is inferred from the directory name by default.
# Optional: IMAGE_TAG, K8S_DIR, CLUSTER_NAME

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

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Manual build/deploy (fallback when Skaffold is not available) ─────────────

.PHONY: build
build: ## Build the Docker image
	docker build -t $(IMAGE) -f $(SERVICE_DIR)/Dockerfile $(PROJECT_ROOT)

.PHONY: load
load: ## Load the image into the k3d cluster
	k3d image import $(IMAGE) -c $(CLUSTER_NAME)

.PHONY: deploy
deploy: ## Apply k8s manifests
	kubectl apply -f $(K8S_DIR)/

.PHONY: build-deploy
build-deploy: build load deploy ## Build, load into k3d, and deploy (manual fallback)

# ── Skaffold ──────────────────────────────────────────────────────────────────

.PHONY: skaffold-dev
skaffold-dev: ## Watch this service — rebuild and redeploy on file change
	skaffold dev -f $(SERVICE_DIR)/skaffold.yaml -p local

.PHONY: skaffold-run
skaffold-run: ## Build and deploy this service once
	skaffold run -f $(SERVICE_DIR)/skaffold.yaml -p local

.PHONY: skaffold-delete
skaffold-delete: ## Remove resources deployed by skaffold
	skaffold delete -f $(SERVICE_DIR)/skaffold.yaml
