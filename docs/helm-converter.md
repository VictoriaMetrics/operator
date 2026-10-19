---
title: Helm to Operator Converter
weight: 100
menu:
  docs:
    parent: operator
    weight: 100
    identifier: helm-converter
aliases:
  - /operator/helm-converter.html
---

The `helm-converter` is a CLI tool designed to help with the migration process from Helm charts to their corresponding VictoriaMetrics Operator Custom Resources (CRs).

It takes your existing Helm `values.yaml` file and generates the equivalent Operator Custom Resource YAML manifest. This manifest is not a 1:1 replacement, but it takes care of the bulk of the conversion work.

## Supported Helm Charts

Currently, the `helm-converter` tool supports the following Helm charts:

*   `victoria-metrics-single`
*   `victoria-metrics-cluster`
*   `victoria-metrics-agent`
*   `victoria-metrics-alert`
*   `victoria-metrics-anomaly`
*   `victoria-metrics-auth`
*   `victoria-logs-single`
*   `victoria-logs-cluster`
*   `victoria-logs-collector`
*   `victoria-traces-single`
*   `victoria-traces-cluster`

Infrastructure components deployed by charts like `victoria-metrics-gateway` or `victoria-logs-multilevel` are currently excluded as they rely on native Kubernetes resources rather than dedicated Operator CRDs.

## Usage

```bash
go run ./cmd/helm-converter -chart <helm-chart-name> -input <path-to-helm-values.yaml> -output <path-to-output-cr.yaml> [options]
```

### Flags

*   `-input` (Required): The path to your input Helm `values.yaml` file.
*   `-output` (Required): The path where the generated Operator CR manifest will be saved.
*   `-chart` (Optional): The name of the Helm chart corresponding to the input values. Defaults to `victoria-metrics-single`.
*   `-name` (Optional): The metadata name for the generated Custom Resource. Defaults to the chart name.
*   `-namespace` (Optional): The metadata namespace for the generated Custom Resource. Defaults to `default`.

## Example

Assume you have a `cluster-values.yaml` from a `victoria-metrics-cluster` Helm deployment:

```yaml
vmselect:
  replicaCount: 2
  image:
    repository: victoriametrics/vmselect
    tag: v1.100.0
```

Run the converter:

```bash
go run ./cmd/helm-converter -input cluster-values.yaml -output vmcluster-cr.yaml -chart victoria-metrics-cluster -name my-vmcluster -namespace monitoring
```

The resulting `vmcluster-cr.yaml` will contain the equivalent `VMCluster` Custom Resource:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: my-vmcluster
  namespace: monitoring
spec:
  vmselect:
    image:
      repository: victoriametrics/vmselect
      tag: v1.100.0
    replicaCount: 2
```

## Notes

The tool maps the majority of critical parameters, including Replicas, Images, Resource Requests/Limits, Affinity, NodeSelectors, Tolerations, ExtraArgs/ExtraEnvs, PersistentVolumes, and specific behavioral flags. Chart-specific behavior is described below.

### License

**Supported CRs.** The charts' `license` and `global.license` blocks become `spec.license` of every CR that supports it. `victoria-logs-single` is converted into the deprecated `VLogs` CR, so its license is currently not converted.

**Precedence.** The license is resolved the same way the charts do:

1. A plaintext key wins over any Secret reference: `license.key`, then `global.license.key`. So `global.license.key` also wins over `license.secret`.
2. Otherwise, the Secret name and key are each taken from the first set value of `license.secret`, `global.license.secret`, `license.keyRef` and `global.license.keyRef`. If either of them is not set anywhere, no license is generated.

### victoria-metrics-alert

**Legacy values.** The chart's legacy aliases are resolved before conversion, the same way the chart does. When both a legacy and a current key are set, the legacy value wins.

| Legacy value | Converted into |
|---|---|
| `server.remote.read`, `server.remote.write` | `server.remoteRead`, `server.remoteWrite` |
| `server.notifier.alertmanager`, `server.notifiers[].alertmanager` | the notifier entry itself |
| nested `basicAuth.{username,password}`, `bearer.{token,tokenFile}` in `server.datasource` and the legacy blocks above | flat `basicAuth.username`, `basicAuth.password`, `bearerToken`, `bearerTokenFile` |
| list-valued `server.notifier.url` | one `server.notifiers` entry per URL |

**Credentials.** Plaintext `basicAuth.username`, `basicAuth.password` and `bearerToken` values of the datasource, remote read, remote write and notifier blocks are moved into generated `Secret` manifests, one per block, referenced from the CR. `bearerTokenFile` is kept as a file path.

**Rules.** `server.config.alerts.groups` becomes a standalone `VMRule` CR, appended to the output as an additional YAML document. The `VMAlert` CR's `spec.ruleSelector` is set to match it. The chart's `rule` entry in `server.extraArgs` is dropped, so rules from `server.configMap` or from extra rule files are not converted.

**Notifiers.** The result depends on which notifier values are set:

- A notifier URL is set: `server.notifier.url` becomes `spec.notifier`, or one `spec.notifiers` entry per URL when it is a list, and `server.notifiers` becomes `spec.notifiers`.
- No notifier URL and no alerting rules: no notifier is generated, since the chart configures none either.
- No notifier URL, but `server.config.alerts.groups` contains alerting rules: the conversion fails. The operator ignores alerting rules of a `VMAlert` without notifiers, while the chart's vmalert refuses to start. Set `server.notifier.url` or `server.notifiers`, or add `notifier.blackhole` to `server.extraArgs`.
- `alertmanager.enabled: true`, with neither `server.notifier.url` nor `server.extraArgs.notifier.url` set: the conversion fails. Here the chart sends alerts to its bundled Alertmanager, besides any `server.notifiers`, and that Alertmanager cannot be converted, since the converter generates only a `VMAlert` from this chart, not a `VMAlertmanager`. To fix it, point `server.notifier.url` at an Alertmanager, such as a separately created `VMAlertmanager`, or set `alertmanager.enabled: false`.
- `alertmanager.enabled: true`, with `server.extraArgs.notifier.url` set: the chart sends alerts to that URL, not to its bundled Alertmanager. The conversion succeeds and keeps the flag in `spec.extraArgs`.
### victoria-metrics-auth

**Users.** The chart's `config` value, vmauth's own native config file, is parsed too:

- Each entry under `config.users` becomes a standalone `VMUser` CR.
- `config.unauthorized_user` becomes the `VMAuth` CR's `spec.unauthorizedUserAccessSpec`.

**Output.** The generated `VMUser` manifests are appended after the `VMAuth` one as additional YAML documents, separated by `---`.

**User selection.** The `VMAuth` CR's `spec.userSelector` is set to match a label applied to each generated `VMUser`, so the operator actually loads them. Without it, a `VMAuth`'s default selectors match nothing.

## Limitations

The generated manifests are not a 1:1 copy of what the chart renders:

- **Ingress and ServiceMonitors.** The charts' `ingress` and `serviceMonitor` values are ignored. For `VMAuth`, set `spec.ingress` or `spec.httpRoute` on the CR; for other components, manage `Ingress` objects separately. The operator creates a `VMServiceScrape` for every component it manages by default, so `serviceMonitor` values usually need no replacement.
- **Autoscaling and PDBs.** The charts' `hpa`, `vpa` and `podDisruptionBudget` values are ignored. Set `spec.hpa`, `spec.vpa` or `spec.podDisruptionBudget` on the CR where it supports them, or create those objects separately.
- **Object names.** `fullnameOverride` and `nameOverride` are ignored. The CR is named after the `-name` flag, which defaults to the chart name, and the operator derives the names of the objects it creates from it, such as `vmsingle-<name>`. These names can differ from the ones the chart used.
- **Chart defaults.** The values are merged with the default `values.yaml` of the latest released chart version, not of the version they were written for.
