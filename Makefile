IMAGE   ?= ghcr.io/pelotech/ghcr-replicator
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CHART   := charts/ghcr-replicator

.PHONY: test lint build image run-dry chart-lint chart-template
test:
	go test -race -count=1 -shuffle=on ./...

lint:
	gofmt -l . | (! grep .) && go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/ghcr-replicator ./cmd/ghcr-replicator

image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

# Plan against real registries without writing anything.
run-dry: build
	./bin/ghcr-replicator -config config.example.yaml -dry-run -log-level debug

chart-lint:
	helm lint --strict $(CHART) -f $(CHART)/ci/test-values.yaml
	helm template t $(CHART) -f $(CHART)/ci/test-values.yaml > /dev/null
	@# Defaults must refuse to render: no registry, rules or credentials.
	@! helm template t $(CHART) > /dev/null 2>&1 || (echo "chart rendered with empty defaults" && exit 1)

chart-template:
	helm template t $(CHART) -f $(CHART)/ci/test-values.yaml
