# AGENTS.md

## Repo map

```
apps/<namespace>/services/<name>/   # one service per directory
├── main/                            # Go service entrypoint (only if Go)
│   ├── main.go
│   └── handler.go
├── Dockerfile                       # build context = project root
├── Makefile                         # single include line (see below)
├── k8s/                             # deployment.yaml, service.yaml, ingress.yaml
└── package.json                     # only for Astro frontend services

infrastructure/
├── k3d/k3d.sh                       # cluster create/delete
├── Makefile/service.mk              # shared build/deploy targets
├── terraform/                       # All infrastructure (MongoDB, monitoring, namespaces)
└── mongodb/deploy.sh                # unused standalone script (Terraform-managed now)

pkg/                                 # shared Go packages (logger, setup, auth, mongo)
packages/ui/                         # @homelab/ui Astro primitive library
```

## Commands

```sh
# Full dev cycle (requires running `make up` first)
make dev             # k3d cluster → terraform infra → build+deploy all services

# Cluster lifecycle
make up              # create k3d cluster
make down            # delete k3d cluster
make infra           # terraform apply + Traefik metrics + copy MongoDB secret

# Service lifecycle (run from any service dir)
make build-deploy    # docker build → k3d import → kubectl apply

# Bulk operations
make deploy-all      # build+load+deploy every discovered service
make restart-all     # rollout restart all deployments
```

## Build conventions

- **Docker build context is project root**, not the service directory. The `Dockerfile` references paths relative to root.
- **Go services**: listen on `:8080` (set by `setup.Default`). K8s Service maps `80 → 8080`.
- **Astro services**: Node build → nginx serving `/dist` on port 80.
- **Image naming**: `homelab/<service-name>:latest` (inferred from directory name by `service.mk`).
- **`imagePullPolicy: IfNotPresent`** on all deployments — images loaded via `k3d image import`.
- Go base image: `golang:1.25-alpine` builder → `alpine:3.21` runtime.
- Node base image: `node:26-alpine` builder → `nginx:alpine`.

## Service Makefiles

Every per-service Makefile is a single include:

```makefile
# Go service:
PROJECT_ROOT := ../../../../
include ../../../../infrastructure/Makefile/service.mk

# Astro:
PROJECT_ROOT = $(abspath ../../../..)
SERVICE_DIR = .
include ../../../../infrastructure/Makefile/service.mk
```

`SERVICE_NAME` and `NAMESPACE` are auto-inferred (`NAMESPACE` from `apps/<name>/...` path; `SERVICE_NAME` from directory name). Infers Go vs Node by presence of `package.json`.

## Observability

### Traces (OpenTelemetry OTLP gRPC)
- `OTEL_EXPORTER_OTLP_ENDPOINT=jaeger.monitoring.svc:4317` set on gateway, users, example-service deployments
- `pkg/trace` provides OTLP gRPC trace exporter + HTTP middleware (creates spans per request)
- Jaeger all-in-one deployed in `monitoring` namespace, ingress at `jaeger.homelab.local`
- Every service uses `trace.Middleware(metrics.Middleware(mux))` via `setup.Run`

### Metrics (Prometheus)
- `pkg/metrics` exposes: `http_requests_total{method,path,status}`, `http_request_duration_seconds{method,path}`, `http_requests_in_flight`
- `/metrics` endpoint added automatically by `setup.Run` via `promhttp.Handler()`
- Go runtime metrics from default Prometheus registry
- ServiceMonitors (with `release: kps` label required by Prometheus operator):
  - `gateway` (auth) — scrapes `:http/metrics`
  - `users` (auth) — scrapes `:http/metrics`
  - `example-service` (test) — scrapes `:http/metrics`
  - `traefik` (monitoring) — scrapes `:9100/metrics`
- Prometheus operator selects ServiceMonitors via `serviceMonitorSelector.matchLabels.release: kps`

### Traefik Metrics
- HelmChartConfig in `kube-system` enables prometheus metrics on port 9100
- Traefik service patched to expose `metrics` port 9100
- ServiceMonitor in `monitoring` namespace scrapes it

## Auth system

- **Traefik ForwardAuth**: `auth-forward-auth` Middleware in `auth` namespace. Any Ingress can use it via annotation `traefik.ingress.kubernetes.io/router.middlewares: auth-forward-auth@kubernetescrd`.
- The `/verify` endpoint on gateway returns a **302 redirect to login** (not 401) so unauthenticated browser users get redirected seamlessly.
- Cookie is set with `Domain: homelab.local` so it works on all subdomains.
- Gateway calls the users service internal via `USERS_SERVICE=http://users` (port 80).
- Users service auto-seeds admin on first startup from `ADMIN_EMAIL`/`ADMIN_PASSWORD` env vars.

## Frontend

- npm workspaces at root. Shared primitives in `packages/ui/` (`@homelab/ui`), consumed via Vite alias (not workspace exports) to avoid `.astro` resolution issues across packages.
- Tailwind v4: `@source "../"` in shared CSS so JIT scans `packages/ui/` for class usage.
- App-specific components go in `apps/<app>/services/ui/src/components/`, not in `packages/ui/`.

## Infra (Terraform at `infrastructure/terraform/`)

### Architecture (local-exec for all native K8s resources)
The Terraform Kubernetes provider (`hashicorp/kubernetes` v2.32.0 and v2.38.0) **hangs on all write operations** (Create) against k3d v1.33.6's API server. The helm provider works fine. Therefore:
- **Namespaces**: `terraform_data` + `local-exec` with `kubectl create namespace --dry-run=client -o yaml | kubectl apply -f -`
- **MongoDB Secret, Service, StatefulSet**: `terraform_data` + `local-exec` with inline YAML piped to `kubectl apply -f -`
- **Helm releases** (kube-prometheus-stack, Jaeger, Loki, Fluent Bit): `helm_release` resource — works fine
- **random_password**: used for MongoDB root password and Grafana admin password
- **Provider auth**: explicit client certificate/key/CA from k3d kubeconfig (decoded from `client-certificate-data`/`client-key-data`/`cluster-ca-data`), `0.0.0.0` → `127.0.0.1` in server URL. `config_path` causes provider crash. `insecure=true` conflicts with `cluster_ca_certificate`.

### Terraform state contents
- 5× `terraform_data` (auth, home, test, monitoring, mongodb namespaces + mongodb_secret, mongodb_service, mongodb_statefulset via local-exec)
- 2× `random_password` (mongodb, grafana)
- 4× `helm_release` (kube-prometheus-stack, jaeger, loki, fluent-bit)

### Monitoring stack
- kube-prometheus-stack (Prometheus + Grafana), Jaeger v2, Loki, Fluent Bit — all via `helm_release` into `monitoring` namespace.
- Prometheus operator selects ServiceMonitors via `serviceMonitorSelector.matchLabels.release: kps`.
- Grafana: `admin` / password in `kps-grafana` K8s Secret, ingress at `grafana.homelab.local`.
- Jaeger: OTLP gRPC `jaeger.monitoring.svc:4317`, OTLP HTTP `:4318`, UI at `jaeger.homelab.local`.
- Traefik metrics: Pre-enabled in k3d, but the `metrics` port must be added to the Traefik service manually (`kubectl patch svc -n kube-system traefik`). A `ServiceMonitor` in `monitoring` namespace scrapes it.

### MongoDB
- StatefulSet `mongo:8` deployed by `terraform_data` (local-exec kubectl apply)
- Secret `mongodb` in `mongodb` namespace with `MONGO_INITDB_ROOT_PASSWORD`, `MONGO_URI`, `MONGO_DB`
- MongoDB secret is copied to `auth`, `finance`, `test` namespaces (as both `mongodb` and `mongo` names) via `infrastructure/copy-mongo-secret.sh` (run by `make infra`)
- Deployments reference it as `mongo` via `envFrom.secretRef`

### Traefik metrics
- HelmChartConfig in `infrastructure/traefik-metrics.sh` (applied by `make infra`)
- Requires `kubectl patch svc traefik -n kube-system` to add metrics port

## DNS

All subdomains must resolve to `127.0.0.1`. Currently configured in `/etc/hosts`. Run via `sudo`:

```sh
sudo sed -i '' '/homelab.local/d' /etc/hosts && \
echo '127.0.0.1 homelab.local auth.homelab.local grafana.homelab.local jaeger.homelab.local finance.homelab.local' | \
sudo tee -a /etc/hosts
```

## Known issues

### kube-prometheus-stack upgrade hangs
Any change to the `kube_prometheus_stack` helm_release (even `create_namespace: false → true`) triggers an upgrade that hangs for 2+ minutes due to CRD processing. Workaround: avoid changing it, or set `create_namespace = true` and leave it unchanged. If stuck in `pending-upgrade`, rollback via `helm rollback kps <revision> -n monitoring`.

## Local dev

- `k3d` cluster must be running (`k3d cluster list` to check).
- No lint/typecheck/test commands exist yet.
- No CI, no pre-commit hooks.
