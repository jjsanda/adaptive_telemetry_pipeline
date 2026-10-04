# Upstream Contribution (Phase 7)

The Phase 7 goal is at least one real, merged contribution to
[`opentelemetry-collector-contrib`](https://github.com/open-telemetry/opentelemetry-collector-contrib).
This page is the playbook: a strategy, a ready-to-adapt PR template, and an honest note on
what can only be decided at contribution time.

## Honest preamble

- **The specific issue must be chosen against the *current* tracker.** contrib moves fast
  (a release roughly every two weeks); any issue number written here today would likely be
  stale or wrong. Pick a live one when you actually open the PR — this doc deliberately
  fabricates no issue number.
- **The maintainer opens the PR under their own identity.** The repo owner submits, signs the
  [CNCF CLA](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/CONTRIBUTING.md),
  and engages with reviewers. This template is a starting point to adapt, not something to
  post verbatim.

## Strategy: start small, land it, then grow

Pick the smallest change that is unambiguously useful, so the first PR is about *learning the
contribution workflow* (CLA, `chloggen`, component code owners, CI) rather than winning a
design debate:

1. **A `good first issue`.** Filter the tracker by the
   [`good first issue`](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22)
   label — these are pre-vetted as small and well-scoped by maintainers.
2. **A documentation fix.** A wrong default, a missing config field in a component README, or a
   broken link. Low risk, still requires the full workflow, and genuinely helpful.
3. **A small, well-scoped bug or feature in an existing component.** Good candidates are ones
   this project already understands deeply — `spanmetricsconnector` (directly comparable to our
   `redmetrics`) or `filterprocessor` (used in our triage pipeline). A missing dimension option,
   an off-by-one in bucket handling, or a clarified error message are all realistic.

Avoid, for a first contribution: proposing a brand-new component (a long governance process
needing a sponsor), or anything touching a stable API.

## Before opening the PR

- Read [`CONTRIBUTING.md`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/CONTRIBUTING.md)
  and the component's `README.md` code-owner list; ping an owner on the issue first.
- One logical change per PR. Run `make gotidy`, `make golint`, and the component's tests locally.
- Add a `.chloggen/<short-name>.yaml` entry (CI enforces it) — see below.

## Ready-to-open PR template

```markdown
### Title
[processor/filter] Clarify error when <field> is <condition>

### Description / Motivation
Fixes #<PICK-A-LIVE-ISSUE>.

<One or two sentences: what is wrong today and why it matters to a user.
Link the issue that motivates it.>

### Changes
- <The specific code change, in one line.>
- <Any config/validation change.>

### Testing
- Added/updated a table-driven test in `<component>/<file>_test.go` covering <case>.
- `go test ./<component>/... ` passes; `-race` clean.
- `make golint` and `make gotidy` clean.

### Documentation
- Updated `<component>/README.md` config table / example if behaviour changed.
```

### CHANGELOG entry (`chloggen`)

contrib does not edit `CHANGELOG.md` directly; add a file at `.chloggen/<short-name>.yaml`:

```yaml
change_type: bug_fix        # breaking | deprecation | new_component | enhancement | bug_fix
component: filterprocessor  # the component's directory name
note: Clarify the validation error emitted when <field> is <condition>.
issues: [<PICK-A-LIVE-ISSUE>]
subtext: |
  Optional longer explanation for release notes.
```

## Definition of done

A PR merged into `opentelemetry-collector-contrib` under the maintainer's identity, with the
issue linked, a `chloggen` entry, and passing CI — then note it in the project
[roadmap](../README.md#roadmap) and [CHANGELOG](../CHANGELOG.md).
