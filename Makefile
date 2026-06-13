SHELL := /bin/zsh

.DEFAULT_GOAL := help

K3D_SCRIPT := infrastructure/k3d/k3d.sh
TERRAFORM := terraform

.PHONY: up
up: ## Create the k3d dev cluster
	$(K3D_SCRIPT) homelab

.PHONY: down
down: ## Delete the k3d dev cluster
	$(K3D_SCRIPT) homelab delete

.PHONY: infra
infra: ## Deploy shared infrastructure (MongoDB, monitoring, Traefik metrics)
	$(TERRAFORM) -chdir=infrastructure/terraform/ apply

SERVICES := $(shell find apps -name Makefile -path "*/services/*" -exec dirname {} \;)

.PHONY: deploy-all
deploy-all: ## Build, load, deploy, and restart every service
	@for dir in $(SERVICES); do \
		echo "\033[36m>>> $$dir\033[0m"; \
		$(MAKE) -C "$$dir" build-deploy || true; \
	done
	$(MAKE) restart-all

.PHONY: restart-all
restart-all: ## Restart all deployments (pick up new images)
	@for dir in $(SERVICES); do \
		ns=$$(echo "$$dir" | awk -F/ '{print $$2}'); \
		svc=$$(basename "$$dir"); \
		kubectl rollout restart deployment "$$svc" -n "$$ns" 2>/dev/null || true; \
	done

.PHONY: dev
dev: up infra deploy-all ## Full cycle: cluster + infra + all services

.PHONY: reset
reset: down up infra deploy-all

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
