# Contributing

This is primarily a portfolio project, but the workflow is the one I'd use on a
team — so contributions and issues are welcome.

## Prerequisites
- Go 1.25+ (the repo pins the toolchain via `GOTOOLCHAIN`; `GOTOOLCHAIN=auto`
  will fetch it for you)
- Docker + Docker Compose
- Node 22 and Python 3.12 (only if you touch those services)

## Common tasks
Run `make` to see everything. The important ones:

```bash
make test              # component unit tests (race + coverage)
make collector-validate# build adaptive-otelcol with ocb and validate the config
make up                # bring up the full local stack
make demo              # drive the anomaly → triage loop end to end
make diagrams          # render the Mermaid diagrams
make k8s-validate      # kubeconform the Kubernetes manifests
```

## Before you push
CI runs exactly what `make` runs locally — no drift. Please make sure these pass:

```bash
make test              # -race -cover, all component packages
make lint              # golangci-lint (v2 config in .golangci.yml)
make collector-validate
```

New Collector components should follow the existing layout (`config.go` /
`factory.go` / `<component>.go` / `metadata.yaml` / `_test.go`), declare their
`consumer.Capabilities{MutatesData: ...}` honestly, and ship table-driven tests.
