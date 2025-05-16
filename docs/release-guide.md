---
weight: 12
title: Release process guidance
menu:
  docs:
    parent: "operator"
    weight: 12
---

## Pre-reqs

1. Make sure you have singing key configured

## Release version and Docker images

1. Make sure all the changes are documented in [CHANGELOG.md](https://github.com/VictoriaMetrics/operator/blob/master/docs/CHANGELOG.md).
   Ideally, every change must be documented in the commit with the change. Alternatively, the change must be documented immediately
   after the commit, which adds the change.
1. Make sure that the release branch has no security issues.
1. Update config-reloader image in [config.go](https://github.com/VictoriaMetrics/operator/blob/a8dd788070d4c012753f7e8e32a3b13e0c50f9af/internal/config/config.go#L108) with the name of new tag.
1. Run `make docs` in order to update variables documentation files.
1. Run `make build-installer` to build Github tag artefacts. Everything inside `/dist` should be included to the release.
1. Cut new version in [CHANGELOG.md](https://github.com/VictoriaMetrics/VictoriaMetrics/blob/master/docs/CHANGELOG.md)
1. create release tag with `git tag vX.X.X` command and push it to the origin - `git push origin vX.X.X`
1. Go to github [Releases Page](https://github.com/VictoriaMetrics/operator/releases) and `Draft new Release` with the name of pushed tag.
1. Update the release description with the content of [CHANGELOG](https://github.com/VictoriaMetrics/operator/blob/master/docs/CHANGELOG.md) for this release.


## Release follow-up

 Wait until github CI job [Release](https://github.com/VictoriaMetrics/operator/actions/workflows/release.yaml) finishes.

### Bump the version of images

The helm chart repository [https://github.com/VictoriaMetrics/helm-charts/](https://github.com/VictoriaMetrics/helm-charts/)

Merge [PR](https://github.com/VictoriaMetrics/helm-charts/pulls) with the following name pattern - `Automatic update operator crds from VictoriaMetrics/operator@*` if there is any.
Bump `tag` field in `values.yaml` with new release version.
Bump `appVersion` field in `Chart.yaml` with new release version.
Add new line to "Next release" section in `CHANGELOG.md` about version update (the line must always start with "`-`"). Do **NOT** change headers in `CHANGELOG.md`.
Bump `version` field in `Chart.yaml` with incremental semver version (based on the `CHANGELOG.md` analysis). 

Do these updates to the following charts:

1. Update `operator` chart version in [`values.yaml`](https://github.com/VictoriaMetrics/helm-charts/tree/master/charts/victoria-metrics-operator/values.yaml) and [`Chart.yaml`](https://github.com/VictoriaMetrics/helm-charts/blob/master/charts/victoria-metrics-operator/Chart.yaml) 
1. Update `crds` chart version in [`values.yaml`](https://github.com/VictoriaMetrics/helm-charts/tree/master/charts/victoria-metrics-operator-crds/Chart.yaml)

1. Push changes to the origin and release `operator` and `crds` charts by triggering [Release Charts](https://github.com/VictoriaMetrics/helm-charts/actions) CI action.
1. Wait for CI finish
1. Bump `appVersion` field in [k8s-stack](https://github.com/VictoriaMetrics/helm-charts/tree/master/charts/victoria-metrics-k8s-stack) `Chart.yaml` with new release version.
1. bump `dependencies` with `name: victoria-metrics-operator` to follow version of recently release [operator](https://github.com/VictoriaMetrics/helm-charts/tree/master/charts/victoria-metrics-operator/Chart.yaml) chart

