import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_SLOW_MS,
  parseBaggageHeader,
  resolveChaos,
} from './chaos.js';

test('parseBaggageHeader parses W3C baggage, ignoring metadata', () => {
  assert.deepEqual(parseBaggageHeader(undefined), {});
  assert.deepEqual(parseBaggageHeader(''), {});
  assert.deepEqual(
    parseBaggageHeader('chaos.target=inventory,chaos.mode=slow,chaos.value=250'),
    { 'chaos.target': 'inventory', 'chaos.mode': 'slow', 'chaos.value': '250' },
  );
  // Whitespace and `;`-delimited metadata are tolerated.
  assert.deepEqual(
    parseBaggageHeader(' chaos.target=all ; meta=1 , chaos.mode=error '),
    { 'chaos.target': 'all', 'chaos.mode': 'error' },
  );
});

test('resolveChaos targets this service or "all"', () => {
  const slow = { 'chaos.target': 'inventory', 'chaos.mode': 'slow', 'chaos.value': '250' };
  assert.deepEqual(resolveChaos(slow, 'inventory'), { mode: 'slow', valueMs: 250 });
  assert.deepEqual(resolveChaos({ ...slow, 'chaos.target': 'all' }, 'inventory'), {
    mode: 'slow',
    valueMs: 250,
  });
});

test('resolveChaos ignores other targets and unknown modes', () => {
  assert.equal(resolveChaos({ 'chaos.target': 'pricing', 'chaos.mode': 'error' }, 'inventory'), null);
  assert.equal(resolveChaos({}, 'inventory'), null);
  assert.equal(
    resolveChaos({ 'chaos.target': 'inventory', 'chaos.mode': 'wat' }, 'inventory'),
    null,
  );
});

test('resolveChaos handles error mode and default slow delay', () => {
  assert.deepEqual(
    resolveChaos({ 'chaos.target': 'all', 'chaos.mode': 'error' }, 'inventory'),
    { mode: 'error', valueMs: 0 },
  );
  // Missing/invalid chaos.value falls back to the shared default (800ms).
  assert.deepEqual(
    resolveChaos({ 'chaos.target': 'inventory', 'chaos.mode': 'slow' }, 'inventory'),
    { mode: 'slow', valueMs: DEFAULT_SLOW_MS },
  );
  assert.deepEqual(
    resolveChaos(
      { 'chaos.target': 'inventory', 'chaos.mode': 'slow', 'chaos.value': 'oops' },
      'inventory',
    ),
    { mode: 'slow', valueMs: DEFAULT_SLOW_MS },
  );
});
