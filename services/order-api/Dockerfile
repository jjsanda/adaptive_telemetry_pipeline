# Build context is the ../services directory so the shared module (referenced
# via a replace directive) is available to the build.
FROM golang:1.25 AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY shared/ ./shared/
COPY order-api/ ./order-api/
WORKDIR /src/order-api
RUN go build -trimpath -ldflags="-s -w" -o /out/order-api .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/order-api /order-api
EXPOSE 8080
ENTRYPOINT ["/order-api"]
