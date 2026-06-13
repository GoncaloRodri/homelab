#!/usr/bin/env zsh
set -euo pipefail

# Enable Traefik metrics via HelmChartConfig
cat <<'EOF' | kubectl apply -f -
apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesContent: |-
    metrics:
      prometheus:
        addEntryPointsLabels: true
        addServicesLabels: true
        entryPoint: metrics
EOF

# Patch Traefik service to add metrics port
kubectl patch svc -n kube-system traefik -p '{
  "spec": {
    "ports": [
      {"name": "metrics", "port": 9100, "targetPort": "metrics"}
    ]
  }
}' 2>/dev/null || {
  # If the port already exists, the above might fail; append instead
  echo "Port may already exist, skipping service patch..."
}

echo "Traefik metrics configured (port 9100)."
