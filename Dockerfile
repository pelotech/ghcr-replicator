# Build on the runner's native platform and cross-compile for each target
# (TARGETOS/TARGETARCH are set by buildx), so multi-arch needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
      go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/ghcr-replicator ./cmd/ghcr-replicator

# distroless/static: CA certs, no shell, runs as nonroot (65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ghcr-replicator /ghcr-replicator
USER 65532:65532
ENTRYPOINT ["/ghcr-replicator"]
