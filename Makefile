SHELL := /bin/zsh

.DEFAULT_GOAL := help

K3D_SCRIPT  := infrastructure/k3d/k3d.sh
TERRAFORM   := terraform
REGISTRY    := git.gugagr.xyz/admin

# ── Cluster ───────────────────────────────────────────────────────────────────

.PHONY: up
up: ## Create the k3d dev cluster
	$(K3D_SCRIPT) homelab

.PHONY: down
down: ## Delete the k3d dev cluster
	$(K3D_SCRIPT) homelab delete

# ── Infrastructure ────────────────────────────────────────────────────────────

.PHONY: infra
infra: ## Deploy shared infrastructure (MongoDB, monitoring, Traefik)
	$(TERRAFORM) -chdir=infrastructure/terraform/ apply

# ── Services (Skaffold) ───────────────────────────────────────────────────────

.PHONY: dev
dev: ## Watch all services — rebuild and redeploy on file change
	skaffold dev

.PHONY: run
run: ## Build and deploy all services once (local)
	skaffold run

.PHONY: deploy
deploy: ## Build for ARM64, push to Gitea, and deploy all services to VPS
	skaffold run -p ci --default-repo $(REGISTRY)

.PHONY: deploy-%
deploy-%: ## Build and deploy a single service to VPS (e.g. make deploy-finance-api)
	skaffold run -p ci -m $* --default-repo $(REGISTRY)

.PHONY: dev-finance
dev-finance: ## Watch finance API only
	skaffold dev -f apps/finance/services/api/skaffold.yaml -p local

.PHONY: dev-gateway
dev-gateway: ## Watch auth gateway only
	skaffold dev -f apps/auth/services/gateway/skaffold.yaml -p local

.PHONY: dev-users
dev-users: ## Watch auth users only
	skaffold dev -f apps/auth/services/users/skaffold.yaml -p local

.PHONY: dev-example
dev-example: ## Watch test example-service only
	skaffold dev -f apps/test/services/example-service/skaffold.yaml -p local

# ── Tests ─────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run all unit tests
	go test ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires Docker for testcontainers)
	go test -tags integration ./...

# ── Lifecycle ─────────────────────────────────────────────────────────────────

.PHONY: bootstrap
bootstrap: up infra run ## Full bootstrap: cluster + infra + all services

.PHONY: reset
reset: down bootstrap ## Tear down and rebuild everything from scratch

# ── Help ──────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
