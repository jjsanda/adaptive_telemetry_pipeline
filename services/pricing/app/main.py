"""pricing -- a FastAPI microservice that prices a single order line.

Called by the Go ``fulfillment-worker`` over HTTP as part of one distributed
trace (Go -> pricing) propagated with W3C trace context + baggage. It is the
demo's primary anomaly source: when the edge sets chaos baggage targeting
``pricing`` the service slows down (or errors), producing the latency spike the
Collector's anomaly detector is built to catch.
"""

import asyncio
import hashlib
import os
from dataclasses import dataclass

from fastapi import FastAPI, HTTPException
from opentelemetry import baggage, metrics, trace
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.trace import Status, StatusCode
from pydantic import BaseModel, Field

from app.otel import SERVICE_NAME, SERVICE_VERSION, setup_otel

# Bootstrap OTel (providers + trace-correlated logger) before instrumenting.
log = setup_otel()
tracer = trace.get_tracer(SERVICE_NAME)
meter = metrics.get_meter(SERVICE_NAME)

# Custom instruments required by the spec: a request counter and a price histogram.
REQUESTS = meter.create_counter(
    "pricing.requests",
    unit="1",
    description="Count of /price requests handled, tagged by sku and outcome.",
)
PRICE_USD = meter.create_histogram(
    "price.usd",
    unit="USD",
    description="Computed order-line price in USD.",
)

# Deterministic base prices for the demo's coffee catalogue (matches order-api).
BASE_PRICES: dict[str, float] = {
    "SKU-COFFEE": 12.50,
    "SKU-MUG": 8.00,
    "SKU-BEANS": 15.75,
    "SKU-FILTER": 4.25,
    "SKU-GRINDER": 39.99,
}

# Contract shared with the Go services: default "slow" delay when chaos.value is
# absent (see services/shared/chaos.go).
CHAOS_DEFAULT_SLOW_MS = 800


def base_price(sku: str) -> float:
    """Return a stable base price for ``sku``.

    Known SKUs use the fixed catalogue; unknown SKUs get a deterministic price in
    ``[5.00, 99.99]`` derived from a SHA-256 of the SKU. SHA-256 (rather than the
    builtin ``hash``, which is salted per process) keeps values stable across
    runs so tests never flake.
    """
    if sku in BASE_PRICES:
        return BASE_PRICES[sku]
    n = int.from_bytes(hashlib.sha256(sku.encode("utf-8")).digest()[:4], "big")
    return round(5.0 + (n % 9500) / 100.0, 2)


class PriceRequest(BaseModel):
    sku: str = Field(min_length=1)
    quantity: float = Field(gt=0)


class PriceResponse(BaseModel):
    price_usd: float
    currency: str = "USD"


@dataclass(frozen=True)
class _Chaos:
    mode: str
    delay_ms: int


def _read_chaos() -> "_Chaos | None":
    """Read chaos instructions from W3C baggage set by the upstream.

    The FastAPI instrumentation has already extracted inbound baggage into the
    active context, so ``baggage.get_baggage`` sees what order-api set. Returns a
    fault only when this service is the target (its own name or ``all``).
    """
    target = baggage.get_baggage("chaos.target")
    if target not in (SERVICE_NAME, "all"):
        return None
    mode = baggage.get_baggage("chaos.mode") or ""
    if mode not in ("slow", "error"):
        return None
    delay_ms = CHAOS_DEFAULT_SLOW_MS
    raw = baggage.get_baggage("chaos.value")
    if raw:
        try:
            delay_ms = int(raw)
        except (TypeError, ValueError):
            pass
    return _Chaos(mode=mode, delay_ms=delay_ms)


app = FastAPI(title="pricing", version=SERVICE_VERSION)


@app.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/price", response_model=PriceResponse)
async def create_price(req: PriceRequest) -> PriceResponse:
    span = trace.get_current_span()
    span.set_attribute("pricing.sku", req.sku)
    span.set_attribute("pricing.quantity", float(req.quantity))

    chaos = _read_chaos()
    if chaos is not None:
        span.set_attribute("chaos.injected", True)
        span.set_attribute("chaos.mode", chaos.mode)
        if chaos.mode == "error":
            REQUESTS.add(1, {"sku": req.sku, "outcome": "error", "chaos.injected": True})
            span.set_status(Status(StatusCode.ERROR, "injected fault via chaos baggage"))
            log.error("pricing fault injected", extra={"sku": req.sku, "chaos_mode": "error"})
            raise HTTPException(status_code=500, detail="pricing: injected fault via chaos baggage")
        # mode == "slow": add latency so downstream sees a spike.
        span.set_attribute("chaos.delay_ms", chaos.delay_ms)
        await asyncio.sleep(chaos.delay_ms / 1000.0)

    price_usd = round(base_price(req.sku) * req.quantity, 2)
    span.set_attribute("pricing.price_usd", price_usd)

    REQUESTS.add(1, {"sku": req.sku, "outcome": "ok", "chaos.injected": chaos is not None})
    PRICE_USD.record(price_usd, {"sku": req.sku})
    log.info(
        "priced order line",
        extra={
            "sku": req.sku,
            "quantity": req.quantity,
            "price_usd": price_usd,
            "chaos_injected": chaos is not None,
        },
    )
    return PriceResponse(price_usd=price_usd, currency="USD")


# Instrument after routes exist. This adds server spans and -- crucially --
# extracts W3C trace context + baggage from inbound headers into the active
# context, which is how ``_read_chaos`` observes the upstream's chaos baggage.
FastAPIInstrumentor.instrument_app(app)


def main() -> None:
    import uvicorn

    port = int(os.environ.get("PORT", "8083"))
    uvicorn.run(app, host="0.0.0.0", port=port, log_level="info")


if __name__ == "__main__":
    main()
