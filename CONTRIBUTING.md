
## Required programs

for developing you need:

- kubebuilder v4
- golang 1.23+
- docker
- kubectl

## installing local env

```bash
# make install-tools
```

## local build and run

Use `make build` - it will generate new crds and build binary

for running locally on kind you need to run:

```bash
make deploy-kind
```

or you can run it from IDE with ```main.go```

## publish changes

before creating merge request, ensure that tests passed locally:

```bash
make build # it will update crds
make lint # linting project
make test #unit tests
make test-e2e #e2e tests with minikube
```

## adding new api

For adding new kind - `KIND_NAME`, you have to execute command:

```bash
kubebuilder create api --group operator --version v1beta1 --kind KIND_NAME
```

For adding new webhook for a `KIND_NAME`, you have to execute command:

```bash
kubebuilder create webhook --group operator --version v1beta1 --kind KIND_NAME --conversion --programmatic-validation
```

This will scaffold api and controller. Then you have to edit code at `api/operator/v1beta1` and `internal/controller/operator` folder.
