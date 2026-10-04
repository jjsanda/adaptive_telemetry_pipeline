# Security Policy

## Reporting a vulnerability
This is a personal portfolio project. If you spot a security issue, please open a
GitHub issue describing it, or email the maintainer (see the profile on the repo
owner's GitHub account). I'll respond as soon as I reasonably can.

## Notes on this project's security posture
- **No secrets in the repo.** The optional Anthropic backend reads its API key from
  the `ANTHROPIC_API_KEY` environment variable and the Collector config uses
  `configopaque` so the key is redacted from any logged configuration.
- **Offline by default.** The pipeline runs end to end with zero API keys; the LLM
  backend defaults to a deterministic offline engine.
- **Least privilege.** Container images are distroless/non-root; the Kubernetes RBAC
  grants only the read access `k8sattributes` needs.
- **LLM output is untrusted.** Triage responses are parsed and validated before they
  become log records.
