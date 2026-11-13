# Upstream to Harbor

This file tells you which code goes to [goharbor/harbor](https://github.com/goharbor/harbor)
and how to move it. The goal: the Harbor PR is a copy and an import path change.

Harbor reference commit: [`8c17ef6`](https://github.com/goharbor/harbor/tree/8c17ef6ede52bf95c8b98c530d5d50cb315a134d).

## Layout

`internal/harbor/` has the same tree as Harbor `src/`. The package names are
the same as in Harbor. Code outside `internal/harbor/` is for the replicator only.

| Path in this repo | Harbor path | Status |
|---|---|---|
| `internal/harbor/pkg/reg/adapter/githubcr/packages.go` | `src/pkg/reg/adapter/githubcr/packages.go` | **New. Goes upstream.** |
| `internal/harbor/pkg/reg/adapter/githubcr/packages_test.go` | `src/pkg/reg/adapter/githubcr/packages_test.go` | **New. Goes upstream.** |
| `internal/harbor/pkg/reg/adapter/githubcr/replicator.go` | — | Replicator only. Do not copy. |
| `internal/harbor/pkg/reg/util/pattern.go` | `src/pkg/reg/util/pattern.go` | Copied from Harbor. Keep in sync. |
| `internal/harbor/controller/replication/flow/flatten.go` | `replaceNamespace` in `src/controller/replication/flow/stage.go` | Copied from Harbor. Keep in sync. |
| `internal/harbor/common/http/` | `src/common/http/` | Copied subset (`Client`, `Error`, `GetAndIteratePagination`, `SameOrigin`). |
| `internal/harbor/common/http/modifier/` | `src/common/http/modifier/` | Copied. |
| `internal/harbor/pkg/registry/auth/basic/` | `src/pkg/registry/auth/basic/` | Copied. |
| `internal/harbor/lib/link.go` | `ParseLinks` in `src/lib/link.go` | Copied subset (used by `GetAndIteratePagination`). |

Files that go upstream start with `// UPSTREAM-CANDIDATE`.

## Rules for code that goes upstream

- Import only Harbor packages (the copies in `internal/harbor`) and the standard library.
  Do not import replicator packages.
- Use Harbor helpers: `common_http.Client.GetAndIteratePagination` (pagination and the
  same-origin check), `common_http.Error`, `util.Match` and `util.IsSpecificPath`.
- Use the Harbor style: no `context` in adapter methods, errors like
  `failed to ...: %w`, tests with `testify` (`assert`, `require`) and `Test_name` names.
- Do not put replicator features (logging, user agent, config) in these files.
  Put them in `replicator.go`.
- Do not add concurrency to these files. In Harbor, `FetchArtifacts` already lists
  tags in parallel and the job service does the copies. The replicator does its
  own concurrency in `internal/replicate`.

## The Harbor PR

1. Copy `packages.go` and `packages_test.go` to `src/pkg/reg/adapter/githubcr/`.
2. Change the import paths:

   ```sh
   sed -i 's#github.com/pelotech/ghcr-replicator/internal/harbor/#github.com/goharbor/harbor/src/#' \
     src/pkg/reg/adapter/githubcr/packages*.go
   ```

3. Remove the `UPSTREAM-CANDIDATE` comment and add the Harbor license header.
4. In `adapter.go`, add the `apiURL` field. Set it in `newAdapter`:

   ```go
   type adapter struct {
       client *common_http.Client
       *native.Adapter
       registry *model.Registry
       apiURL   string // new
   }

   // in newAdapter:
       apiURL: githubAPIURL,
   ```

   Also in `newAdapter`, add the basic authorizer to the client only if the registry has a
   credential (`var modifiers []modifier.Modifier` plus `append`, then `modifiers...`).
   Before, a nil authorizer went into the client, and the first API call panicked.

5. In `FetchArtifacts`, replace the error for wildcard filters:

   ```go
   if paths, ok := util.IsSpecificPath(pattern); ok {
       repositories = paths
   } else {
   -   err = errors.New("only support specific repository name")
   -   return
   +   repositories, err = a.listRepositories(pattern)
   +   if err != nil {
   +       return nil, err
   +   }
   }
   ```

6. Add a `FetchArtifacts` test with a wildcard filter (mock the Packages API and
   `/v2/<repo>/tags/list` on the same test server, and set `apiURL` to its URL).
7. Docs: the GHCR registry credential (username and personal access token) now
   also calls the GitHub REST API. The token needs `read:packages`. Pulls from
   ghcr.io already need this scope.

## Known differences from Harbor

| Here | Harbor | Effect on the PR |
|---|---|---|
| Tests use `httptest.NewServer` | Some tests use `test.NewServer` from `src/common/utils/test` | Optional change |
| `common_http.NewClient(nil)` uses `&http.Client{}` | Uses `GetHTTPTransport()` | None |
| `basic.NewAuthorizer` returns `modifier.Modifier` | Returns `lib.Authorizer` (same method) | None |
| `flow.ReplaceNamespace` is exported and has no `dstRepoComponentPathType` | `replaceNamespace` is private and has it | Not upstream code |
| `adapter` struct is in `replicator.go` with two fields | `adapter` struct in `adapter.go` | Step 4 |

## Keep in sync

When Harbor changes a copied file, copy the change here. Check these at each
Harbor release:

- `src/pkg/reg/util/pattern.go` and the `doublestar` version in `src/go.mod`
- `replaceNamespace` in `src/controller/replication/flow/stage.go`
- `src/pkg/reg/adapter/githubcr/adapter.go` (the code that the PR changes)
