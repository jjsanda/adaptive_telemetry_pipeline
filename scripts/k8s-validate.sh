#!/usr/bin/env bash
#
# Render each kustomize overlay and schema-validate the output with kubeconform.
# Invoked by CI (.github/workflows/ci.yml, job "manifests").
#
# kubeconform flags:
#   -strict                 reject unknown/duplicate fields — catches typos in
#                           the fields of the resources it DOES have schemas for
#                           (Namespace, ServiceAccount, ClusterRole[Binding],
#                           Deployment, Service).
#   -summary                print a per-run valid/invalid/skipped summary.
#   -ignore-missing-schemas our CRDs (OpenTelemetryCollector, Instrumentation)
#                           ship no published JSON schema, so kubeconform SKIPS
#                           them instead of failing. Expected — the operator's
#                           own admission webhook validates those at apply time.
#
# Exits non-zero if ANY rendered manifest is invalid.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

OVERLAYS=(
  "deploy/k8s/overlays/kind"
  "deploy/k8s/overlays/prod"
)

rc=0
for overlay in "${OVERLAYS[@]}"; do
  echo "== Validating ${overlay} =="
  if ! kubectl kustomize "${overlay}" \
      | kubeconform -strict -summary -ignore-missing-schemas; then
    echo "!! ${overlay} FAILED validation" >&2
    rc=1
  fi
  echo
done

if [[ "${rc}" -ne 0 ]]; then
  echo "manifest validation FAILED" >&2
fi
exit "${rc}"
