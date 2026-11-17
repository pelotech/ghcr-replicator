# ghcr-replicator

Mirrors container images from **ghcr.io** into **Harbor** using Harbor-style
replication rules, **including wildcard name filters** such as
`my-org/apps/**`.

## Why

Harbor's GHCR adapter rejects wildcard name filters
([`only support specific repository name`](https://github.com/goharbor/harbor/blob/8c17ef6ede52bf95c8b98c530d5d50cb315a134d/src/pkg/reg/adapter/githubcr/adapter.go#L147-L152)).
The generic Docker Registry adapter accepts them, but resolves them by paging
through `/v2/_catalog`, and on ghcr.io that lists every public repository on
GitHub, so the run never finishes.

This tool lists an owner's packages through the **GitHub Packages API**
(`GET /orgs/{org}/packages?package_type=container`, falling back to `/users/{user}/...`),
then applies Harbor's own filter and flattening semantics. The listing code
is written to lift directly into an upstream patch to the GHCR adapter.

## How a run works

For each rule (all rules, or only the rules named with `-rules`):

1. **Resolve repositories.** A specific filter (`org/a`, `org/{a,b}`) is used as-is,
   with no API call. A wildcard filter lists the owner's packages and matches them.
2. **List tags** from the registry and apply the tag filter (`matches` / `excludes`).
3. **Map** each repository into `destNamespace` using Harbor's flattening level.
4. **Copy** tags whose destination digest differs. Digests that already match are
   counted as up to date. If the destination digest differs and `override: false`,
   the tag is skipped. Image indexes (multi-arch) are copied whole.

Failures are counted per tag or repository and don't stop the run. The process
exits with **1** if anything failed, so the Job shows as failed.

## Config

[`config.example.yaml`](config.example.yaml) is the reference for every option,
with defaults and the matching Harbor replication-policy field. Each rule maps
onto one Harbor pull-replication policy. Copy it to start a config.

## Credentials (env)

| var | used for |
|---|---|
| `GHCR_USERNAME`, `GHCR_TOKEN` | ghcr.io pulls and the Packages API (`read:packages`) |
| `HARBOR_USERNAME`, `HARBOR_PASSWORD` | Harbor pushes (robot account with push on each destination project) |

Destination projects must already exist in Harbor.

## Usage

```sh
make test                      # unit tests (race detector), including in-memory registry tests
make build
GHCR_USERNAME=... GHCR_TOKEN=... HARBOR_USERNAME=... HARBOR_PASSWORD=... \
  ./bin/ghcr-replicator -config config.yaml -dry-run     # plan only
./bin/ghcr-replicator -config config.yaml -rules team-apps
```

Flags: `-config`, `-dry-run`, `-rules a,b` (runs only these), `-log-level`, `-timeout` (default 55m), `-version`.
Logs are JSON on stdout.

## Kubernetes (Helm)

Each release publishes the image and a chart at the same version:

```sh
helm install ghcr-replicator oci://ghcr.io/pelotech/charts/ghcr-replicator \
  --version 0.1.0 -n ghcr-replicator --create-namespace -f my-values.yaml
```

Minimal `my-values.yaml`:

```yaml
config:                      # rendered verbatim to config.yaml (see config.example.yaml)
  destination:
    registry: harbor.example.com
  rules:
    - name: team-apps
      filters: {name: "my-org/apps/**", tag: "{,v}[0-9]*.[0-9]*.[0-9]*"}
      destNamespace: apps
      destNamespaceReplace: 2
credentials:
  secretName: ghcr-replicator-credentials   # GHCR_USERNAME, GHCR_TOKEN, HARBOR_USERNAME, HARBOR_PASSWORD
```

The chart deploys a hardened `CronJob` (non-root, read-only root filesystem,
no service-account token, `concurrencyPolicy: Forbid`), the config
`ConfigMap`, and a `ServiceAccount`. The chart refuses to render without a
destination registry, at least one rule, and `credentials.secretName`. Other useful values:
`schedule`, `suspend`, `dryRun`, `image.digest`,
`imagePullSecrets` (needed while the image is private). See
[`values.yaml`](charts/ghcr-replicator/values.yaml).

`make chart-lint` lints and renders the chart with `ci/test-values.yaml`.

The pod needs egress to `api.github.com`, `ghcr.io` (and its blob CDN
`pkg-containers.githubusercontent.com`), and Harbor.

## Not implemented (vs Harbor)

Label filters, resource-type filters, the "Enable rule" switch (remove a rule to stop it), `deletion` (removing destination tags
that disappeared upstream), and untagged artifacts.

## Credits

Everything under `internal/harbor` mirrors the
[goharbor/harbor](https://github.com/goharbor/harbor) `src/` tree (Apache-2.0):
code copied from Harbor, plus the new GHCR adapter code meant for an upstream
PR. See [`UPSTREAM.md`](UPSTREAM.md).
