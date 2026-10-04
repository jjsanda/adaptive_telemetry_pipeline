#!/usr/bin/env bash
# Steady background load against the order pipeline. Ctrl-C to stop.
# Pass an argument to also inject a chaos scenario, e.g.:
#   load/loadgen.sh "chaos=pricing&mode=slow&ms=900"
set -euo pipefail
API=${API:-http://localhost:8080}
QUERY=${1:-}
url="$API/"; [ -n "$QUERY" ] && url="$API/?$QUERY"
echo "Load against $url (Ctrl-C to stop)"
while true; do curl -s -o /dev/null "$url" || true; sleep 0.3; done
