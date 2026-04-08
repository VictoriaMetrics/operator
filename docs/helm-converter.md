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

# Helm to Operator Converter

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

The tool maps the majority of critical parameters, including Replicas, Images, Resource Requests/Limits, Affinity, NodeSelectors, Tolerations, ExtraArgs/ExtraEnvs, PersistentVolumes, and specific behavioral flags. 

## Limitations

Helm converter manifests are not 1:1

Some configurations are currently excluded from automated mapping:
*   **Ingress and ServiceMonitors**: Secondary standalone objects sometimes managed by the Helm charts (like raw `Ingress` objects or default `ServiceMonitor` definitions) are not processed. The Operator typically assumes you manage `Ingress` resources externally or define `VMServiceScrape` logic independently.
*   **Autoscaling and PDBs**: Fields defining `hpa` (HorizontalPodAutoscaler), `vpa` (VerticalPodAutoscaler), and `podDisruptionBudget` are not automatically translated to their embedded Operator CR equivalents at this time. These manifests need to be created manually in case the operator's CR doesn't provide a way to define those.
*   **`fullname` and templating**: Operator doesn't support redefining object names with `fullname`, so resources managed by the operator may have different names.
