// Chaos-injection contract, matching the Go `shared.ApplyChaos` implementation.
//
// The upstream sets W3C baggage members:
//   chaos.target  — a service.name that should misbehave, or "all"
//   chaos.mode    — "slow" | "error"
//   chaos.value   — slow delay in milliseconds (default 800)
//
// We read those members from the propagated baggage. Parsing is kept as pure
// functions so the whole contract is unit-testable without a live Collector.

export const CHAOS_TARGET_KEY = 'chaos.target';
export const CHAOS_MODE_KEY = 'chaos.mode';
export const CHAOS_VALUE_KEY = 'chaos.value';

export const DEFAULT_SLOW_MS = 800;

export type ChaosMode = 'slow' | 'error';

export interface ChaosPlan {
  mode: ChaosMode;
  /** Delay in milliseconds for `slow`; 0 for `error`. */
  valueMs: number;
}

/**
 * Parse a raw W3C `baggage` header (RFC-style `k1=v1,k2=v2`) into a map.
 * Property/metadata segments after a `;` are ignored, and values are
 * percent-decoded. Returns an empty object for undefined/empty input.
 */
export function parseBaggageHeader(header: string | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  if (!header) {
    return out;
  }
  for (const rawMember of header.split(',')) {
    const member = rawMember.trim();
    if (!member) {
      continue;
    }
    // Drop any `;`-delimited baggage metadata before splitting key=value.
    const kv = member.split(';', 1)[0] ?? '';
    const eq = kv.indexOf('=');
    if (eq <= 0) {
      continue;
    }
    const key = kv.slice(0, eq).trim();
    const value = kv.slice(eq + 1).trim();
    if (!key) {
      continue;
    }
    out[key] = safeDecode(value);
  }
  return out;
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

/**
 * Decide whether this service should inject a fault given the propagated
 * baggage entries. Returns null when not targeted or the mode is unknown.
 *
 * Mirrors the Go contract: fire when chaos.target equals our service name or
 * "all"; "slow" uses chaos.value ms (default 800); "error" is signalled with
 * valueMs 0.
 */
export function resolveChaos(
  baggage: Record<string, string>,
  serviceName: string,
): ChaosPlan | null {
  const target = baggage[CHAOS_TARGET_KEY];
  if (!target || (target !== serviceName && target !== 'all')) {
    return null;
  }
  const mode = baggage[CHAOS_MODE_KEY];
  if (mode === 'error') {
    return { mode: 'error', valueMs: 0 };
  }
  if (mode === 'slow') {
    return { mode: 'slow', valueMs: parseDelayMs(baggage[CHAOS_VALUE_KEY]) };
  }
  return null;
}

function parseDelayMs(raw: string | undefined): number {
  if (raw === undefined || raw === '') {
    return DEFAULT_SLOW_MS;
  }
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n >= 0 ? n : DEFAULT_SLOW_MS;
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
