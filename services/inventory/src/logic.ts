// Deterministic stock logic. No randomness — the same SKU always yields the same
// availability, so traces/tests are reproducible.

export interface StockResult {
  in_stock: boolean;
  available: number;
}

// In-memory "stock map": availability is derived deterministically from the SKU
// and memoized here, standing in for a real inventory store.
const stock = new Map<string, number>();

// Stable 32-bit FNV-1a hash of a string. Deterministic across processes/runs.
export function hashSku(sku: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < sku.length; i++) {
    h ^= sku.charCodeAt(i);
    // FNV prime multiply, kept in 32-bit unsigned range via Math.imul.
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

// Stable per-SKU availability in the range [10, 59]: (hash mod 50) + 10.
export function availableForSku(sku: string): number {
  const cached = stock.get(sku);
  if (cached !== undefined) {
    return cached;
  }
  const available = (hashSku(sku) % 50) + 10;
  stock.set(sku, available);
  return available;
}

export function checkStock(sku: string, quantity: number): StockResult {
  const available = availableForSku(sku);
  return { available, in_stock: available >= quantity };
}
