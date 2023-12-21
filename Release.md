### Helm charts release

#### Bump the version of images.

1. Need to update [`values.yaml`](https://github.com/VictoriaMetrics/helm-charts/blob/master/charts/victoria-metrics-operator/values.yaml), 
2. Specify the correct version in [`Chart.yaml`](https://github.com/VictoriaMetrics/helm-charts/blob/master/charts/victoria-metrics-operator/Chart.yaml)
3. Update version [README.md](https://github.com/VictoriaMetrics/helm-charts/blob/master/charts/victoria-metrics-operator/README.md), specify the new version in the documentation
4. Push changes to master. `master` is a source of truth
5. Rebase `master` into `gh-pages` branch
6. Run `make package` which creates or updates zip file with the packed chart
7. Run `make merge`. It creates or updates metadata for charts in index.yaml
8. Push the changes to `gh-pages` branch


### Operator Hub release

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
