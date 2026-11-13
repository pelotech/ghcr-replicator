module github.com/pelotech/ghcr-replicator

go 1.26.1

require (
	github.com/bmatcuk/doublestar v1.3.4 // pinned to Harbor's version; see internal/harbor/pkg/reg/util
	github.com/google/go-containerregistry v0.22.1
	github.com/stretchr/testify v1.12.1
	golang.org/x/sync v0.25.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/docker/cli v29.7.2+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)
