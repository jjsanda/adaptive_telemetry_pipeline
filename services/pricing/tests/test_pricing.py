"""Tests for the pricing service.

They run entirely in-process via Starlette's TestClient with no live Collector:
the OTLP exporters are constructed but never need to connect (they fail quietly
in a background thread if they do), so importing and serving the app is enough.
"""

import time

from fastapi.testclient import TestClient

from app.main import app, base_price

client = TestClient(app)


def test_healthz():
    r = client.get("/healthz")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}


def test_price_known_sku_math():
    r = client.post("/price", json={"sku": "SKU-COFFEE", "quantity": 2})
    assert r.status_code == 200
    assert r.json() == {"price_usd": 25.0, "currency": "USD"}


def test_price_unknown_sku_is_deterministic():
    payload = {"sku": "SKU-DOES-NOT-EXIST", "quantity": 3}
    r1 = client.post("/price", json=payload)
    r2 = client.post("/price", json=payload)
    assert r1.status_code == 200
    # No randomness -> identical inputs give identical outputs.
    assert r1.json() == r2.json()
    assert r1.json()["price_usd"] == round(base_price("SKU-DOES-NOT-EXIST") * 3, 2)


def test_price_rejects_non_positive_quantity():
    r = client.post("/price", json={"sku": "SKU-MUG", "quantity": 0})
    assert r.status_code == 422


def test_chaos_error_returns_500():
    headers = {"baggage": "chaos.target=pricing,chaos.mode=error"}
    r = client.post("/price", json={"sku": "SKU-MUG", "quantity": 1}, headers=headers)
    assert r.status_code == 500


def test_chaos_for_other_service_is_ignored():
    # A fault targeting a different service must not affect pricing.
    headers = {"baggage": "chaos.target=inventory,chaos.mode=error"}
    r = client.post("/price", json={"sku": "SKU-MUG", "quantity": 1}, headers=headers)
    assert r.status_code == 200
    assert r.json()["price_usd"] == 8.0


def test_chaos_slow_adds_latency():
    delay_ms = 150
    headers = {"baggage": f"chaos.target=all,chaos.mode=slow,chaos.value={delay_ms}"}
    start = time.perf_counter()
    r = client.post("/price", json={"sku": "SKU-BEANS", "quantity": 1}, headers=headers)
    elapsed = time.perf_counter() - start
    assert r.status_code == 200
    assert r.json()["price_usd"] == 15.75
    # Lower bound only (asyncio.sleep guarantees at least the delay); never a
    # flaky upper bound.
    assert elapsed >= (delay_ms / 1000.0) * 0.8
