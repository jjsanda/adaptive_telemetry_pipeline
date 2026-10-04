#!/usr/bin/env bash
#
# Install the cluster prerequisites for the adaptive-telemetry-pipeline k8s
# deployment, in order:
#
#   1. cert-manager      — the OpenTelemetry Operator serves admission webhooks
#                          over TLS and relies on cert-manager to issue and
#                          rotate the serving certs + inject the CA bundle.
#                          The operator is REQUIRED to have it; without it the
#                          operator pod never goes Ready.
#   2. OTel Operator     — the controller that reconciles our
#                          OpenTelemetryCollector and Instrumentation CRs into
#                          Deployments/DaemonSets and SDK auto-injection.
#
# Versions are PINNED (repo policy, verified 2026-07-04). The operator and its
# CRD schema move together, so bump OTEL_OPERATOR_VERSION deliberately.
set -euo pipefail

# --- pinned versions -------------------------------------------------------
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.16.2}"
OTEL_OPERATOR_VERSION="${OTEL_OPERATOR_VERSION:-v0.154.0}"

CERT_MANAGER_URL="https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
OTEL_OPERATOR_URL="https://github.com/open-telemetry/opentelemetry-operator/releases/download/${OTEL_OPERATOR_VERSION}/opentelemetry-operator.yaml"

# --- 1. cert-manager -------------------------------------------------------
echo ">> Installing cert-manager ${CERT_MANAGER_VERSION}"
kubectl apply -f "${CERT_MANAGER_URL}"

echo ">> Waiting for cert-manager to become ready"
# The webhook is the long pole; the operator's CA injection depends on it.
kubectl -n cert-manager rollout status deploy/cert-manager           --timeout=180s
kubectl -n cert-manager rollout status deploy/cert-manager-webhook   --timeout=180s
kubectl -n cert-manager rollout status deploy/cert-manager-cainjector --timeout=180s

# --- 2. OpenTelemetry Operator --------------------------------------------
echo ">> Installing OpenTelemetry Operator ${OTEL_OPERATOR_VERSION}"
# First apply can race the freshly-installed cert-manager webhook (the operator
# manifest itself carries a MutatingWebhookConfiguration whose CA is injected by
# cert-manager). Retry once after a short pause to absorb that race.
kubectl apply -f "${OTEL_OPERATOR_URL}" \
  || { echo ">> retrying operator apply after webhook warm-up"; sleep 15; kubectl apply -f "${OTEL_OPERATOR_URL}"; }

echo ">> Waiting for the operator to become ready"
kubectl -n opentelemetry-operator-system rollout status \
  deploy/opentelemetry-operator-controller-manager --timeout=180s

echo ">> Prerequisites installed: cert-manager ${CERT_MANAGER_VERSION}, OTel Operator ${OTEL_OPERATOR_VERSION}"
