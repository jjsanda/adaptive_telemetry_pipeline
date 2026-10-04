# Multi-stage build of the custom adaptive-otelcol distribution.
# Build context is the repository root so the local `components` module (referenced
# by the manifest's `path: ./components`) is available to ocb.
FROM golang:1.25 AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src
COPY components/ ./components/
COPY collector/builder-config.yaml ./collector/builder-config.yaml
RUN go install go.opentelemetry.io/collector/cmd/builder@v0.155.0 \
 && builder --config collector/builder-config.yaml

FROM gcr.io/distroless/static:nonroot
COPY --from=build /src/collector/_build/adaptive-otelcol /adaptive-otelcol
COPY collector/config/collector.yaml /etc/otel/collector.yaml
EXPOSE 4317 4318 8889 13133
ENTRYPOINT ["/adaptive-otelcol"]
CMD ["--config", "/etc/otel/collector.yaml"]
