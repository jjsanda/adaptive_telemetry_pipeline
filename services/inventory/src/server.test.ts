// Integration test: boots the real Express app on an ephemeral port and drives
// it over HTTP. No OTel SDK is loaded here (the test file is run directly, not
// via the instrumentation preload), which proves the server starts and responds
// even with no Collector reachable, and that chaos baggage is honored via the
// raw-header fallback.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import type { AddressInfo } from 'node:net';
import type { Server } from 'node:http';
import { createApp } from './server.js';

let server: Server;
let base: string;

interface StockBody {
  in_stock: boolean;
  available: number;
}

before(async () => {
  server = createApp().listen(0) as Server;
  await new Promise<void>((resolve) => server.once('listening', resolve));
  const { port } = server.address() as AddressInfo;
  base = `http://127.0.0.1:${port}`;
});

after(() => {
  server.close();
});

const postCheck = (body: unknown, headers: Record<string, string> = {}): Promise<Response> =>
  fetch(`${base}/check`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
  });

test('GET /healthz returns 200 ok', async () => {
  const res = await fetch(`${base}/healthz`);
  assert.equal(res.status, 200);
  assert.deepEqual(await res.json(), { status: 'ok' });
});

test('POST /check returns deterministic availability', async () => {
  const res = await postCheck({ sku: 'SKU-COFFEE', quantity: 1 });
  assert.equal(res.status, 200);
  const body = (await res.json()) as StockBody;
  assert.equal(typeof body.available, 'number');
  assert.equal(body.in_stock, body.available >= 1);

  // Same SKU -> identical availability on a second call.
  const again = (await (await postCheck({ sku: 'SKU-COFFEE', quantity: 1 })).json()) as StockBody;
  assert.equal(again.available, body.available);
});

test('POST /check rejects malformed input with 400', async () => {
  const res = await postCheck({ sku: 123 });
  assert.equal(res.status, 400);
});

test('chaos error via baggage header returns 500', async () => {
  const res = await postCheck(
    { sku: 'SKU-MUG', quantity: 1 },
    { baggage: 'chaos.target=inventory,chaos.mode=error' },
  );
  assert.equal(res.status, 500);
  const body = (await res.json()) as { error: string };
  assert.match(body.error, /injected fault/);
});

test('chaos slow via baggage header delays the response', async () => {
  const started = Date.now();
  const res = await postCheck(
    { sku: 'SKU-MUG', quantity: 1 },
    { baggage: 'chaos.target=all,chaos.mode=slow,chaos.value=200' },
  );
  const elapsed = Date.now() - started;
  assert.equal(res.status, 200);
  assert.ok(elapsed >= 180, `expected ~200ms delay, got ${elapsed}ms`);
});

test('untargeted chaos is ignored', async () => {
  const started = Date.now();
  const res = await postCheck(
    { sku: 'SKU-MUG', quantity: 1 },
    { baggage: 'chaos.target=pricing,chaos.mode=slow,chaos.value=500' },
  );
  const elapsed = Date.now() - started;
  assert.equal(res.status, 200);
  assert.ok(elapsed < 200, `should not delay when another service is targeted, took ${elapsed}ms`);
});
