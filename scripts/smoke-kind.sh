#!/usr/bin/env bash
#
# End-to-end smoke test on a local kind cluster: stand up the operator, deploy
# the kind overlay, drive traffic, induce an anomaly, and assert the adaptive
# loop produced a triage line in the gateway log.
#
# ============================ RAM WARNING ==================================
# A single kind node + cert-manager + the OTel Operator + gateway + agent
# DaemonSet + the demo app needs roughly 2.5-3 GiB of RAM. This project's dev
# box is memory-constrained (frequently <1.5 GiB free, swap already in use), so
# `up` REFUSES to run unless it sees >= MIN_FREE_MIB of free memory. This keeps
# the box from OOM-killing your session. Run it on a machine with >= 4 GiB free,
# or override with SMOKE_FORCE=1 (expect heavy swap / OOM on a small box).
# ==========================================================================
#
# Usage:
#   scripts/smoke-kind.sh up      # create + deploy + drive + assert
#   scripts/smoke-kind.sh down    # delete the kind cluster
#
# Tunables (env): CLUSTER, DISTRO_IMAGE, APP_IMAGE, MIN_FREE_MIB, SMOKE_FORCE.
set -euo pipefail

CLUSTER="${CLUSTER:-adaptive-smoke}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# Placeholder distro image the collectors run (built + side-loaded locally).
DISTRO_IMAGE="${DISTRO_IMAGE:-ghcr.io/jjsanda/adaptive-otelcol:latest}"
# Demo app image (the Python pricing service).
APP_IMAGE="${APP_IMAGE:-adaptive-pricing:latest}"
# Minimum free RAM (MiB) required before we dare create a cluster.
MIN_FREE_MIB="${MIN_FREE_MIB:-4096}"

require_ram() {
  local avail
  avail="$(free -m | awk '/^Mem:/ {print $7}')"   # "available" column
  echo ">> Available memory: ${avail} MiB (need >= ${MIN_FREE_MIB} MiB)"
  if [[ "${avail}" -lt "${MIN_FREE_MIB}" && "${SMOKE_FORCE:-0}" != "1" ]]; then
    echo "!! Not enough free RAM for a kind cluster on this box — aborting." >&2
    echo "   Run on a machine with >= ${MIN_FREE_MIB} MiB free, or set" >&2
    echo "   SMOKE_FORCE=1 to override (expect OOM / swap thrash)." >&2
    exit 1
  fi
}

load_image() {
  # Side-load a locally-built image into the kind node so pods with
  # imagePullPolicy: Never/IfNotPresent can start without a registry.
  local img="$1"
  if docker image inspect "${img}" >/dev/null 2>&1; then
    echo ">> Loading ${img} into kind"
    kind load docker-image --name "${CLUSTER}" "${img}"
  else
    echo "!! Image ${img} not found locally. Build/tag it first, e.g.:" >&2
    echo "     make collector && docker tag <built> ${DISTRO_IMAGE}" >&2
    echo "     docker build -t ${APP_IMAGE} services/pricing" >&2
    exit 1
  fi
}

up() {
  require_ram

  echo ">> Creating kind cluster '${CLUSTER}'"
  kind create cluster --name "${CLUSTER}" --wait 120s

  echo ">> Installing cert-manager + OTel Operator"
  bash "${SCRIPT_DIR}/install-operator.sh"

  load_image "${DISTRO_IMAGE}"
  load_image "${APP_IMAGE}"

  echo ">> Applying the kind overlay"
  kubectl apply -k deploy/k8s/overlays/kind

  echo ">> Waiting for the operator to reconcile the workloads"
  # The operator turns our CRs into these Deployments/DaemonSets.
  kubectl -n observability rollout status deploy/gateway-collector --timeout=180s
  kubectl -n apps          rollout status ds/agent-collector       --timeout=180s
  kubectl -n apps          rollout status deploy/pricing           --timeout=180s

  echo ">> Port-forwarding the pricing service"
  kubectl -n apps port-forward svc/pricing 8083:8083 >/tmp/adaptive-pf.log 2>&1 &
  local pf_pid=$!
  # shellcheck disable=SC2064
  trap "kill ${pf_pid} 2>/dev/null || true" EXIT
  sleep 4

  # anomaly detector needs a baseline: warmup=8 samples * redmetrics flush 5s
  # ~= 40s minimum before a series can be flagged. Drive ~60s of steady load.
  echo ">> Driving ~60s of baseline traffic (building the EWMA baseline)"
  for _ in $(seq 1 60); do
    curl -s -o /dev/null -X POST localhost:8083/price \
      -H 'content-type: application/json' \
      -d '{"sku":"SKU-COFFEE","quantity":2}' || true
    sleep 1
  done

  # Chaos baggage targeting pricing -> the service sleeps -> latency spike ->
  # redmetrics RED histogram -> anomaly EWMA flags it -> filter -> llmtriage.
  echo ">> Inducing an anomaly (chaos baggage: pricing slows down)"
  for _ in $(seq 1 40); do
    curl -s -o /dev/null -X POST localhost:8083/price \
      -H 'content-type: application/json' \
      -H 'baggage: chaos.target=pricing,chaos.mode=slow,chaos.value=1500' \
      -d '{"sku":"SKU-COFFEE","quantity":2}' || true
    sleep 1
  done

  echo ">> Looking for a triage/anomaly line in the gateway log"
  # llmtriage emits a correlated log record -> logs/triage -> debug exporter.
  if kubectl -n observability logs deploy/gateway-collector --tail=1000 \
       | grep -iE 'triage|anomaly|is_anomaly'; then
    echo ">> SUCCESS: the adaptive loop produced triage output."
  else
    echo "!! No triage line yet — the loop can lag 1-2 min. Inspect live with:" >&2
    echo "   kubectl -n observability logs deploy/gateway-collector -f" >&2
    exit 1
  fi
}

down() {
  echo ">> Deleting kind cluster '${CLUSTER}'"
  kind delete cluster --name "${CLUSTER}"
}

case "${1:-}" in
  up)   up ;;
  down) down ;;
  *)    echo "usage: $0 {up|down}" >&2; exit 2 ;;
esac
