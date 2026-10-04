#!/usr/bin/env bash
# Render every Mermaid diagram in docs/diagrams/src/*.mmd to SVG (transparent)
# and PNG (white, 2x) using mermaid-cli. GitHub renders the README's inline
# Mermaid natively — these exports exist for contexts that can't (slides,
# previews). Requires Node/npx; downloads a headless Chromium on first run.
set -euo pipefail
cd "$(dirname "$0")/.."

SRC="docs/diagrams/src"
OUT="docs/diagrams/rendered"
PCONF="scripts/puppeteer-config.json"
MMDC_VERSION="${MMDC_VERSION:-11}"
mkdir -p "$OUT"

for f in "$SRC"/*.mmd; do
  name="$(basename "$f" .mmd)"
  echo "rendering $name ..."
  npx -y "@mermaid-js/mermaid-cli@${MMDC_VERSION}" -i "$f" -o "$OUT/$name.svg" -b transparent -p "$PCONF"
  npx -y "@mermaid-js/mermaid-cli@${MMDC_VERSION}" -i "$f" -o "$OUT/$name.png" -b white -s 2 -p "$PCONF"
done

echo "done -> $OUT"
