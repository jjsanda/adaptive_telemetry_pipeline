# resourcecost

A FinOps/governance **processor** that attaches an owning team and a rough cost estimate to every signal that passes through it.

| Status        |                                   |
| ------------- | --------------------------------- |
| Stability     | `beta`: traces, metrics, logs     |
| Signals       | traces → traces · metrics → metrics · logs → logs (enriched in place) |
| Distributions | `adaptive-otelcol`                |
| Mutates data  | **yes** — writes resource attributes in place |
| Code owners   | @jjsanda                          |

It answers two questions that show up on every large observability bill, for **any** workload (not just GenAI): *who owns this telemetry?* and *roughly what does it cost to ship?* Two resource attributes are added to each resource:

- `telemetry.owner.team` — resolved from a `service.name → team` map.
- `telemetry.cost.estimate` — a USD proxy: serialized bytes ÷ 1024 × `cost_per_kb`.

## Configuration

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `team_by_service` | `map[string]string` | `{}` | Maps `service.name` to the owning team, attached as `telemetry.owner.team`. |
| `default_team` | `string` | `""` | Team attached when a `service.name` has no explicit entry. Empty means leave unattributed. |
| `cost_per_kb` | `float64` | `0.0004` | Cost-proxy rate in USD per KB of serialized signal data. `0` disables cost attribution (and skips the per-resource sizing work). |

`Validate` rejects a negative `cost_per_kb`.

## Example

```yaml
processors:
  resourcecost:
    cost_per_kb: 0.0004
    default_team: platform
    team_by_service:
      order-api: payments-team
      pricing: pricing-team
```

## How it works

For each resource in a batch the processor reads `service.name`, looks up the team (falling back to `default_team`), and writes `telemetry.owner.team`. When `cost_per_kb > 0` it marshals the resource with a `pdata` proto sizer, converts bytes to a USD estimate, and writes `telemetry.cost.estimate` — the marshal is lazy, so a disabled cost estimate costs nothing. Because it writes attributes **in place**, the factory declares `MutatesData: true`, so the pipeline hands it an owned copy at fan-out. See [ADR-0003](../../../docs/adr/0003-data-ownership-and-mutatesdata.md).
