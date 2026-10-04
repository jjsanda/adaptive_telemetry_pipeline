import { test } from 'node:test';
import assert from 'node:assert/strict';
import { availableForSku, checkStock, hashSku } from './logic.js';

test('availability is deterministic and in the [10, 59] range', () => {
  for (const sku of ['SKU-COFFEE', 'SKU-MUG', 'SKU-BEANS', 'abc', '']) {
    const a = availableForSku(sku);
    const b = availableForSku(sku);
    assert.equal(a, b, 'same SKU must return the same availability');
    assert.ok(a >= 10 && a <= 59, `availability ${a} out of range for "${sku}"`);
    assert.ok(Number.isInteger(a));
  }
});

test('different SKUs generally differ; hash is stable', () => {
  assert.equal(hashSku('SKU-COFFEE'), hashSku('SKU-COFFEE'));
  assert.notEqual(availableForSku('SKU-COFFEE'), availableForSku('SKU-GRINDER'));
});

test('in_stock reflects available >= quantity', () => {
  const sku = 'SKU-FILTER';
  const { available } = checkStock(sku, 1);

  assert.deepEqual(checkStock(sku, available), { available, in_stock: true });
  assert.deepEqual(checkStock(sku, available + 1), { available, in_stock: false });
  assert.deepEqual(checkStock(sku, 0), { available, in_stock: true });
});
