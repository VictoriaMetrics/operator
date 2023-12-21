
## Required programs

for developing you need:
- golang 1.15+
- operator-sdk v1.33.0
- docker
- minikube or kind for e2e tests
- golangci-lint


## installing local env

- make install-develop-tools
- kind create cluster

## local build and run

Use `make build` - it will generate new crds and build binary


for running locally you need minikube and run two commands:
```bash
make install
make run
```
or you can run it from IDE with ```main.go```

## publish changes

before creating merge request, ensure that tests passed locally:
```bash
make build # it will update crds
make lint # linting project
make test #unit tests
make e2e-local #e2e tests with minikube
```

## adding new api

For adding new kind - KIND_NAME, you have to execute command:

```bash
operator-sdk create api --group operator --version v1beta1 --kind KIND_NAME
```

This will scaffold api and controller. Then you have to edit code at `api` and `controllers` folder.

## create olm package

Choose version (release tag at github) and generate or update corresponding csv file
```bash
TAG=v0.36.0 make bundle
```

it will generate files at directories: `bundle/`

### publish to operator-hub

checkout to the latest release:
1) `git checkout tags/v0.36.0`
2) build package manifest: `TAG=v0.36.0 make bundle`
3) add replacement for a previous version to generated cluster csv:
`vi bundle/manifests/victoriametrics-operator.clusterserviceversion`
```yaml
spec:
  replaces: victoriametrics-operator.v0.35.0
```
Now you have to fork two repos and create pull requests to them with new version `/bundle`:
1) https://github.com/k8s-operatorhub/community-operators for OperatorHub.io
2) https://github.com/redhat-openshift-ecosystem/community-operators-prod for Embedded OperatorHub in OpenShift and OKD.

