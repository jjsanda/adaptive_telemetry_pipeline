# ADR 0006 — Deploy on Kubernetes via the OpenTelemetry Operator

**Status:** Accepted

## Context
The Collector can be deployed with a plain Deployment/DaemonSet + Helm/Kustomize, or
via the OpenTelemetry Operator, which manages Collectors and auto-instrumentation
through CRDs.

## Decision
Deploy via the **OpenTelemetry Operator** (v0.154.0): an `OpenTelemetryCollector`
(v1beta1) gateway (Deployment) plus a node agent (DaemonSet), and an `Instrumentation`
(v1alpha1) resource that injects SDKs into app pods by annotation. `k8sattributes`
enriches telemetry with pod metadata (backed by least-privilege RBAC).

## Consequences
- Auto-instrumentation with zero app code changes for Java/Python/Node (init-container
  injection), demonstrating the operator's headline feature.
- Two sharp edges worth knowing:
  - **Go is different.** Go auto-instrumentation is eBPF-based, needs a feature gate and
    a privileged sidecar — not the clean init-container model of the other languages.
    For Go services, manual SDK instrumentation is often preferable, which is exactly
    what the demo's Go services do.
  - **Ordering.** The `Instrumentation` resource must exist *before* an app pod starts,
    or injection silently no-ops.
- The Target Allocator can shard Prometheus scrape targets across collector replicas —
  noted as the scaling path for metrics collection.
- Trade-off: the Operator adds a cert-manager dependency and its own upgrade cadence.
