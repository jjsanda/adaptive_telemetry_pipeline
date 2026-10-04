// Capture Grafana / Jaeger / Prometheus screenshots into docs/screenshots/.
// Requires the stack to be running (`make up`) and Playwright installed — the
// `make screenshots` target installs it for you. Each shot is best-effort.
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";

const OUT = "docs/screenshots";
mkdirSync(OUT, { recursive: true });

const shots = [
  { name: "grafana-explore", url: "http://localhost:3000/explore", wait: 4000 },
  { name: "jaeger-search", url: "http://localhost:16686/search", wait: 3000 },
  {
    name: "prometheus-red",
    url: "http://localhost:9090/graph?g0.expr=rate(red_calls_total%5B1m%5D)&g0.tab=0",
    wait: 3000,
  },
];

const browser = await chromium.launch({ args: ["--no-sandbox", "--disable-dev-shm-usage"] });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
for (const s of shots) {
  try {
    await page.goto(s.url, { waitUntil: "networkidle", timeout: 20000 });
    await page.waitForTimeout(s.wait);
    await page.screenshot({ path: `${OUT}/${s.name}.png` });
    console.log(`saved ${OUT}/${s.name}.png`);
  } catch (e) {
    console.error(`skip ${s.name}: ${e.message}`);
  }
}
await browser.close();
