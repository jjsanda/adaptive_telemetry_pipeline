# adaptive_telemetry_pipeline — developer workflows.
# Run `make` or `make help` to list targets.

.DEFAULT_GOAL := help
export GOTOOLCHAIN := go1.25.11
export GOWORK := off

COMPOSE := docker compose -f deploy/compose/docker-compose.yaml
BUILDER_VERSION := v0.155.0
DISTRO := adaptive-otelcol
GO_MODULES := components services/shared services/order-api services/fulfillment-worker

help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-24s\033[0m %s\n",$$1,$$2}'

## ---------- Build ----------
tools: ## Install the OpenTelemetry Collector Builder (ocb)
	go install go.opentelemetry.io/collector/cmd/builder@$(BUILDER_VERSION)

collector: ## Build the custom adaptive-otelcol binary with ocb
	builder --config collector/builder-config.yaml
	@echo "built ./collector/_build/$(DISTRO)"

collector-validate: collector ## Build then validate the full pipeline config
	./collector/_build/$(DISTRO) validate --config collector/config/collector.yaml

build-go: ## Compile the Go services
	go -C services/order-api build ./...
	go -C services/fulfillment-worker build ./...

## ---------- Test / lint ----------
test: ## Run component unit tests (race + coverage)
	go -C components test -race -cover ./...

test-all: ## Run every Go module's tests
	@for m in $(GO_MODULES); do echo "== test $$m =="; go -C $$m test -race -cover ./... || exit 1; done

bench: ## Run benchmarks for the hot paths
	go -C components test -run '^$$' -bench . -benchmem ./...

vet: ## go vet every module
	@for m in $(GO_MODULES); do echo "vet $$m"; go -C $$m vet ./...; done

lint: ## Run golangci-lint if present, else go vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		for m in $(GO_MODULES); do (cd $$m && golangci-lint run ./...); done; \
	else echo "golangci-lint not found; running go vet"; $(MAKE) vet; fi

fmt: ## Format Go code
	@for m in $(GO_MODULES); do (cd $$m && gofmt -w . && go run golang.org/x/tools/cmd/goimports@latest -w . 2>/dev/null || true); done

tidy: ## go mod tidy every module
	@for m in $(GO_MODULES); do echo "tidy $$m"; go -C $$m mod tidy; done

## ---------- Run (Docker Compose) ----------
up: ## Start the full stack (collector + backends + services), building images
	$(COMPOSE) up -d --build
	@echo "order-api http://localhost:8080  Grafana http://localhost:3000  Jaeger http://localhost:16686  Prometheus http://localhost:9090"

demo: ## Run the end-to-end demo: drive load, induce an anomaly, show the triage log
	bash load/demo.sh

load: ## Start the background load generator (opt-in profile)
	$(COMPOSE) --profile load up -d loadgen

ps: ## Show stack status
	$(COMPOSE) ps

logs: ## Tail the collector logs
	$(COMPOSE) logs -f collector

down: ## Stop the stack and remove volumes
	$(COMPOSE) --profile load down -v

## ---------- Kubernetes ----------
k8s-validate: ## Validate manifests with kubeconform (no cluster needed)
	bash scripts/k8s-validate.sh

kind-up: ## Create a kind cluster and deploy a minimal smoke stack
	bash scripts/smoke-kind.sh up

kind-down: ## Delete the kind smoke cluster
	bash scripts/smoke-kind.sh down

## ---------- Docs / diagrams ----------
diagrams: ## Render Mermaid diagrams to SVG/PNG
	bash scripts/render-diagrams.sh

screenshots: ## Capture Grafana/Jaeger screenshots (stack must be up)
	npm install --no-save playwright && npx playwright install --with-deps chromium && node scripts/screenshots.mjs

## ---------- Misc ----------
clean: ## Remove build artifacts
	rm -rf collector/_build

.PHONY: help tools collector collector-validate build-go test test-all bench vet lint fmt tidy \
	up demo load ps logs down k8s-validate kind-up kind-down diagrams screenshots clean
