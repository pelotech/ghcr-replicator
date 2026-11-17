# Build on the runner's native platform and cross-compile for each target
# (TARGETOS/TARGETARCH are set by buildx), so multi-arch needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26.11@sha256:4165ee6bf3b7811fd7b40373290405c83d0167dcb87ee5f29c427cdcef45ff8f AS build
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
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/ghcr-replicator /ghcr-replicator
USER 65532:65532
ENTRYPOINT ["/ghcr-replicator"]
