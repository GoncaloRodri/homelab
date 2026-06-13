#!/usr/bin/env zsh
set -euo pipefail

CLUSTER_NAME="${1:-homelab}"

create() {
  echo "==> Creating k3d cluster '$CLUSTER_NAME' ..."

  k3d cluster create "$CLUSTER_NAME" \
    --servers 1 \
    --agents 1 \
    --port "80:80@loadbalancer" \
    --port "443:443@loadbalancer" \
    --port "30000-30010:30000-30010@loadbalancer" \
    --wait

  echo "==> Cluster '$CLUSTER_NAME' is ready."
  echo ""
  kubectl cluster-info --context "k3d-$CLUSTER_NAME"
}

delete() {
  echo "==> Deleting k3d cluster '$CLUSTER_NAME' ..."
  k3d cluster delete "$CLUSTER_NAME"
  echo "==> Done."
}

case "${2:-}" in
  delete|down)
    delete
    ;;
  *)
    create
    ;;
esac
