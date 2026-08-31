# syntax=docker/dockerfile:1

# The immutable indexes let local checks use a native builder while CI selects amd64.
# The binary target remains linux/amd64, Cloud Run's execution platform.
FROM golang:1.26.4-alpine3.23@sha256:18b460dd17542c2ba43299a633cf6ebfc1115101509531471d7cfce1019af083 AS build

WORKDIR /src
COPY go.mod ./
COPY authoring ./authoring
COPY cmd/tracker-web ./cmd/tracker-web
COPY contract ./contract
COPY identity ./identity
COPY internal/containerfixture ./internal/containerfixture
COPY packet ./packet
COPY packetexport ./packetexport
COPY runtimeexport ./runtimeexport
COPY surface ./surface
COPY tenant ./tenant

ENV CGO_ENABLED=0 \
    GOARCH=amd64 \
    GOOS=linux \
    GOTOOLCHAIN=local

RUN go build -buildvcs=false -trimpath -ldflags='-s -w -buildid=' \
      -o /out/tracker-web ./cmd/tracker-web && \
    go build -buildvcs=false -trimpath -ldflags='-s -w -buildid=' \
      -o /out/containerfixture ./internal/containerfixture

# This target exists only for the local and CI runtime check. It never contributes a
# layer to the default production target.
FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS fixture
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/containerfixture /usr/local/bin/containerfixture
USER 65532:65532
EXPOSE 18080
ENTRYPOINT ["/usr/local/bin/containerfixture"]

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /out/tracker-web /usr/local/bin/tracker-web

ENV PORT=8080
EXPOSE 8080
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=2s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/healthz" || exit 1

ENTRYPOINT ["/usr/local/bin/tracker-web"]
