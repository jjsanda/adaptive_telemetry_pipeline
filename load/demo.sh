#!/usr/bin/env bash
# End-to-end demo of the adaptive loop:
#   traffic -> RED metrics -> EWMA anomaly -> LLM triage -> incident log.
#
# Drives baseline traffic to warm the detector, induces a pricing latency
# anomaly via chaos baggage, then recovers — and tells you where to see the
# LLM-generated triage log.
set -euo pipefail
API=${API:-http://localhost:8080}
COMPOSE="docker compose -f $(dirname "$0")/../deploy/compose/docker-compose.yaml"

hit() { curl -s -o /dev/null "$1" || true; }

echo "Waiting for order-api at $API ..."
for _ in $(seq 1 30); do
  if curl -sf -o /dev/null "$API/healthz"; then break; fi
  sleep 1
done

echo "1/3  Baseline traffic (~60s) to warm up the EWMA detector..."
end=$((SECONDS + 60)); while [ $SECONDS -lt $end ]; do hit "$API/"; sleep 0.25; done

echo "2/3  Inducing a pricing latency anomaly (slow 900ms) for ~30s..."
end=$((SECONDS + 30)); while [ $SECONDS -lt $end ]; do hit "$API/?chaos=pricing&mode=slow&ms=900"; sleep 0.25; done

echo "3/3  Recovering..."
end=$((SECONDS + 15)); while [ $SECONDS -lt $end ]; do hit "$API/"; sleep 0.25; done

echo
echo "The LLM triage summary should now be emitted. See it via:"
echo "  • collector logs : $COMPOSE logs collector | grep -i triage"
echo "  • Grafana (Loki) : http://localhost:3000  ->  Explore  ->  {service_name=\"adaptive-otelcol\"}"
echo "  • Jaeger         : http://localhost:16686  (service: pricing — note the slow spans)"
echo
echo "Recent triage lines from the collector:"
$COMPOSE logs --since 2m collector 2>/dev/null | grep -i "triage\|anomaly" | tail -8 || true
