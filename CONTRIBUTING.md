# Contributing

Contributions follow the [VictoriaMetrics AI policy](https://docs.victoriametrics.com/victoriametrics/contributing/#ai-policy).
You may use AI tools, but you are responsible for every change in your pull request and must be able to explain it.
Write pull request descriptions and comments in your own words. Pull requests that look like unreviewed AI output
may be closed without review.

## Required programs

For development you need:

- Go, the version from `go.mod`
- docker
- kubectl
- kubebuilder v4, only for [adding a new API](#adding-a-new-api)

Other tools, such as `controller-gen`, `golangci-lint` or `kind`, are downloaded to `./bin` by the `make` targets that use them.
To install the code generation tools upfront, run:

```bash
make install-tools
```

## Build and run locally

Build the operator binary to `bin/`:

```bash
make build
```

Run the operator from your host against the cluster in your current kubeconfig context:

```bash
make run
```

You can also run `cmd/main.go` from your IDE.

Or build the images and deploy the operator with webhooks to a local kind cluster, which is created if it doesn't exist:

```bash
make deploy-kind
```

## Before opening a pull request

Run the same checks as CI:

```bash
make lint                      # linters
make manifests api-gen docs    # regenerate CRDs, clients and docs
git status                     # the generated files must be committed
./hack/check-rbac-markers.sh   # RBAC markers
make test                      # unit tests
make test-e2e                  # e2e tests in a kind cluster, optional, see below
```

Describe user-visible changes in [docs/CHANGELOG.md](https://github.com/VictoriaMetrics/operator/blob/master/docs/CHANGELOG.md) under `## tip`.

## CI checks on pull requests

`Build and Test` (lint, generated files and unit tests) runs on every push to a pull request, except for docs-only changes.
If this is your first contribution, workflows run only after a maintainer approves them.

e2e tests don't run on every push. They run on a pull request when:

- a maintainer approves the pull request, unless e2e tests already passed or are running for the latest commit;
- a maintainer adds the `run-e2e` label;
- someone comments `/run-e2e`. Maintainers can do it on any pull request, authors on their own pull request.

To run e2e tests again, e.g. after pushing a fix, comment `/run-e2e` again.
If this is your first contribution, the e2e run also waits for a maintainer to approve it.

e2e tests are skipped for draft pull requests and for changes that can't affect them, such as docs.
Changes to `go.mod`, `go.sum` or the root `Makefile` are tested on master after merge.

## Adding a new API

To add a new kind, e.g. `KIND_NAME`, run:

```bash
kubebuilder create api --group operator --version v1beta1 --kind KIND_NAME
```

To add a webhook for it, run:

```bash
kubebuilder create webhook --group operator --version v1beta1 --kind KIND_NAME --conversion --programmatic-validation
```

This scaffolds the API and the controller. Then edit the code in `api/operator/v1beta1` and `internal/controller/operator`.
