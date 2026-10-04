// HTTP surface for the inventory service.
//
// IMPORTANT: this module imports express, so it must only ever be loaded *after*
// `instrumentation.ts` has started the OTel SDK (see index.ts). It never imports
// the instrumentation itself.
import express, { type Express, type Request, type Response } from 'express';
import {
  context,
  propagation,
  trace,
  metrics,
  SpanStatusCode,
} from '@opentelemetry/api';
import { checkStock } from './logic.js';
import { parseBaggageHeader, resolveChaos, sleep } from './chaos.js';

const SERVICE_NAME = process.env.OTEL_SERVICE_NAME ?? 'inventory';

interface CheckRequestBody {
  sku?: unknown;
  quantity?: unknown;
}

/**
 * Read chaos baggage for the active request. Prefers the OpenTelemetry baggage
 * populated by the HTTP auto-instrumentation (the production path); falls back
 * to parsing the raw `baggage` header directly, which keeps the handler fully
 * functional in unit tests where no SDK/propagator is registered.
 */
function readBaggage(req: Request): Record<string, string> {
  const otelBaggage = propagation.getBaggage(context.active());
  if (otelBaggage) {
    const entries = otelBaggage.getAllEntries();
    if (entries.length > 0) {
      const out: Record<string, string> = {};
      for (const [key, entry] of entries) {
        out[key] = entry.value;
      }
      return out;
    }
  }
  const header = req.headers.baggage;
  const raw = Array.isArray(header) ? header.join(',') : header;
  return parseBaggageHeader(raw);
}

export function createApp(): Express {
  const app = express();
  app.use(express.json());

  const meter = metrics.getMeter(SERVICE_NAME);
  // Custom metric required by the spec: one counter per stock check, tagged with
  // whether the item was in stock.
  const checksCounter = meter.createCounter('inventory.checks', {
    description: 'Number of stock checks performed by the inventory service',
  });

  app.get('/healthz', (_req: Request, res: Response) => {
    res.status(200).json({ status: 'ok' });
  });

  app.post('/check', async (req: Request, res: Response) => {
    const span = trace.getActiveSpan();
    try {
      // --- Chaos injection (matches the Go/Python contract) ---
      const plan = resolveChaos(readBaggage(req), SERVICE_NAME);
      if (plan) {
        span?.setAttribute('chaos.injected', true);
        span?.setAttribute('chaos.mode', plan.mode);
        if (plan.mode === 'error') {
          const err = new Error(`${SERVICE_NAME}: injected fault via chaos baggage`);
          span?.recordException(err);
          span?.setStatus({ code: SpanStatusCode.ERROR, message: err.message });
          res.status(500).json({ error: err.message });
          return;
        }
        // slow
        span?.setAttribute('chaos.value_ms', plan.valueMs);
        await sleep(plan.valueMs);
      }

      // --- Stock check ---
      const { sku, quantity } = req.body as CheckRequestBody;
      if (typeof sku !== 'string' || typeof quantity !== 'number' || !Number.isFinite(quantity)) {
        res.status(400).json({
          error: 'invalid request: expected { sku: string, quantity: number }',
        });
        return;
      }

      const result = checkStock(sku, quantity);
      checksCounter.add(1, { in_stock: result.in_stock });
      span?.setAttribute('inventory.sku', sku);
      span?.setAttribute('inventory.quantity', quantity);
      span?.setAttribute('inventory.available', result.available);
      span?.setAttribute('inventory.in_stock', result.in_stock);

      res.status(200).json(result);
    } catch (err) {
      span?.recordException(err as Error);
      span?.setStatus({ code: SpanStatusCode.ERROR, message: String(err) });
      res.status(500).json({ error: 'internal error' });
    }
  });

  return app;
}

export function start(): ReturnType<Express['listen']> {
  const port = Number(process.env.PORT ?? 8082);
  const app = createApp();
  const server = app.listen(port, () => {
    console.log(`inventory listening on :${port} (service.name=${SERVICE_NAME})`);
  });
  return server;
}
