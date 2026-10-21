---
weight: 12
title: Release process guidance
menu:
  docs:
    parent: "operator"
    weight: 12
---

## Pre-reqs

1. Make sure you have a signing key configured

## Release version and Docker images

1. Make sure all changes since the previous release are listed under `## tip` in [CHANGELOG.md](https://github.com/VictoriaMetrics/operator/blob/master/docs/CHANGELOG.md).
   Ideally, a change is documented in the same commit that makes it. If not, document it right after that commit.
   Along with user-visible changes, document API breaks too.
1. Make sure the release branch has no open security issues. Check the [Security tab](https://github.com/VictoriaMetrics/operator/security).
1. Prepare the release commit:
   1. Set the default `VM_OPERATOR_VERSION` in [internal/config/config.go](https://github.com/VictoriaMetrics/operator/blob/master/internal/config/config.go) to the new version, for example `getVersion("vX.Y.Z")`.
      The operator uses this value to build the default config-reloader image tag (`config-reloader-vX.Y.Z`).
   1. Run `make docs` to regenerate `docs/env.md` and `docs/api.md`.
   1. Cut the new version in [CHANGELOG.md](https://github.com/VictoriaMetrics/operator/blob/master/docs/CHANGELOG.md). Add these lines right below `## tip`:
      ```md
      ## [vX.Y.Z](https://github.com/VictoriaMetrics/operator/releases/tag/vX.Y.Z)
      **Release date:** DD Month YYYY
      ```
   1. Open a PR like `cut release vX.Y.Z` with these changes and merge it.
1. Tag the merged commit and push both tags to the origin:
   ```sh
   git tag vX.Y.Z        # operator
   git tag api/vX.Y.Z    # operator's api Go module (api/go.mod)
   git push origin vX.Y.Z api/vX.Y.Z
   ```
1. Go to the GitHub [Releases page](https://github.com/VictoriaMetrics/operator/releases) and click `Draft a new release` for the `vX.Y.Z` tag.
1. Paste this release's section from [CHANGELOG.md](https://github.com/VictoriaMetrics/operator/blob/master/docs/CHANGELOG.md) into the release description, then publish the release.
   Publishing starts the [Release](https://github.com/VictoriaMetrics/operator/actions/workflows/release.yaml) workflow. It builds, pushes and signs the operator and config-reloader images, builds the OLM bundle,
   and attaches `install-no-webhook.yaml`, `install-with-webhook.yaml`, `crd.yaml` and `olm-bundle.tar.gz` to the release.


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
