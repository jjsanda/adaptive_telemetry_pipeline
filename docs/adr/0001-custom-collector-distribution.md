# ADR 0001 — Build a custom Collector distribution with `ocb`

**Status:** Accepted

## Context
We need to run our own Collector components (Phases 2–5). A stock `otelcol-contrib`
image can't load out-of-tree Go components, and it ships ~everything (large image,
larger attack surface). The OpenTelemetry Collector Builder (`ocb`) compiles a
distribution from a manifest that lists exactly the components we want, including
local ones.

## Decision
Ship a custom distribution, **adaptive-otelcol**, defined by
`collector/builder-config.yaml` and built with `ocb` v0.155.0. Local components live
in one `components` Go module, wired in via the manifest's per-entry `path:` override
(no hand-written `replace`). The image is a multi-stage, distroless build.

## Consequences
- The binary contains only what we use (~70 MB vs. the multi-hundred-MB contrib image):
  smaller, faster to start, smaller attack surface.
- We own the version treadmill. The Collector releases roughly every two weeks, and
  surveys show most deployed Collectors run many versions behind. Keeping a custom
  distro current is real, ongoing work — we pin every module to one consistent line
  (stable `v1.61.0` / beta `v0.155.0`) and a CI job runs `ocb build` + `validate` on
  every push. Automating manifest bumps (e.g. Renovate) is the natural next step.
- We must keep the stable/beta version split straight: `pdata`, `component`,
  `consumer`, and `processor` are stable `v1.x`; `connector` is still `v0.x`.
