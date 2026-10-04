# Build context is the ../services directory so the shared module (referenced
# via a replace directive) is available to the build.
FROM golang:1.25 AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY shared/ ./shared/
COPY fulfillment-worker/ ./fulfillment-worker/
WORKDIR /src/fulfillment-worker
RUN go build -trimpath -ldflags="-s -w" -o /out/fulfillment-worker .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/fulfillment-worker /fulfillment-worker
EXPOSE 8081
ENTRYPOINT ["/fulfillment-worker"]
