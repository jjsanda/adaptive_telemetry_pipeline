# Kubernetes deployment — adaptive-telemetry-pipeline

Runs the custom **adaptive-otelcol** distribution on Kubernetes via the
[OpenTelemetry Operator], in the classic **agent + gateway** topology:

```
  app pods ──(SDK auto-injection)──► gateway (Deployment, observability ns)
     │                                 └─ full adaptive loop, debug exporter
     └──────► agent (DaemonSet, apps ns) ──OTLP──► gateway
```

* **gateway** (`observability`) runs the whole loop from
  `collector/config/collector.yaml` — `redmetrics → anomaly → filter →
  llmtriage` — but every backend exporter is swapped for `debug` (verbosity
  detailed), so the triage log is visible with `kubectl logs`, no Jaeger/Loki/
  Prometheus required. `k8sattributes` is added to the traces & logs pipelines.
* **agent** (`apps`) is a lean DaemonSet: `otlp → k8sattributes + batch → otlp`
  forwarding to the gateway Service.

## Layout

```
base/                 namespaces, RBAC, gateway + agent CRs, Instrumentation, demo app
overlays/kind/        local: imagePullPolicy Never/IfNotPresent, tiny requests
overlays/prod/        replicas: 2 gateway, real limits, pinned registry tags
```

Image tags are overridable through kustomize `images:` — including the CRs'
`spec.image`, via `base/kustomizeconfig/images.yaml`.

## Deploy

```bash
# 1. cluster prereqs — cert-manager + operator (pinned versions)
bash scripts/install-operator.sh

# 2. pick an overlay
kubectl apply -k deploy/k8s/overlays/kind      # or overlays/prod

# validate manifests without a cluster (what CI runs):
bash scripts/k8s-validate.sh

# full local e2e (RAM-heavy, see the script header):
bash scripts/smoke-kind.sh up   # ... then: down
```

## Operational notes

**(a) Go eBPF auto-instrumentation is not free.** Java/Python/Node inject a
zero-touch SDK via an *init container* + `LD_PRELOAD`/`PYTHONPATH` — unprivileged
and per-pod. Go is compiled/statically linked, so there is no runtime agent to
attach; the operator instead attaches **`go-auto`, an eBPF sidecar that needs
`securityContext.privileged: true` (CAP_SYS_PTRACE / kernel uprobes)** and the
operator's `operator.autoinstrumentation.go` feature gate enabled. That
privilege + a per-binary offset dependency is why **manual SDK instrumentation
(as the Go services here already do) is usually preferable for Go** — which is
why the demo auto-injects only the Python `pricing` service.

**(b) The `Instrumentation` CR must exist BEFORE the app pod starts.** Injection
happens once, in the mutating admission webhook, at pod-creation time. If the
pod is admitted before `demo-instr` exists (or it lives in another namespace, or
the `inject-python` annotation is malformed), the webhook **silently no-ops** —
no error, just an un-instrumented pod. Apply `base/` (which includes the CR)
before the workload, and `kubectl rollout restart` any pod that started early.

**(c) Target Allocator for Prometheus scrape sharding.** The gateway is a
Deployment; scale it out and naive Prometheus scraping either double-scrapes or
misses targets. The operator's **Target Allocator** shards scrape targets across
collector replicas (`spec.targetAllocator.enabled: true`, with a Prometheus
receiver), so each replica scrapes a disjoint slice and the set rebalances as
replicas change — the standard way to scale metric collection horizontally.

[OpenTelemetry Operator]: https://github.com/open-telemetry/opentelemetry-operator
