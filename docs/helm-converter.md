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

## Migrating a running Helm release

`helm-converter migrate` automates cutting a running standalone Helm release over to an
operator-managed CR. Unlike the offline `convert` command above, it connects to the cluster
(via kubeconfig) and mutates it directly.

```bash
go run ./cmd/helm-converter migrate -chart victoria-metrics-single -namespace monitoring \
  -release my-release -values values.yaml [-target-name my-release] [-yes] [-dry-run]
```

It discovers the release's existing workloads, Services, and PersistentVolumeClaims by the
standard Helm labels, then runs one of two strategies (`-strategy`):

**`WithDowntime`** (default) — a brief window between deleting the old workload(s) and the
new CR becoming ready:

1. deletes the old Deployment (and any ConfigMap/Secret its pod spec actually referenced),
2. rebinds the existing PersistentVolume under the operator's own PVC name — no data is
   copied, the same volume is reused,
3. creates the target CR and waits for it to become ready,
4. repoints the release's existing Service at the new pods, preserving the Service's name
   and DNS entry.

For cluster charts (`victoria-metrics-cluster`, `victoria-logs-cluster`), the same steps run
once per component (`vmstorage`/`vmselect`/`vminsert`, or `vlstorage`/`vlselect`/`vlinsert`),
discovered via the chart's own `app.kubernetes.io/component` label: each component's old
StatefulSet/Deployment is deleted, each StatefulSet component's PVCs are rebound one per
ordinal (`vmstorage`/`vmselect`, or `vlstorage` — the insert/select-without-cache components
have no persistent storage), then the single target VMCluster/VLCluster CR is created once
and every component's Service is repointed after it becomes ready.

**`NoDowntime`** — never touches the old workload(s) or their PVC(s):

1. deploys a buffering VMAgent (VLAgent for VictoriaLogs) pointed at the old storage's write
   endpoint via an internal alias Service (created solely so the buffer keeps a stable,
   direct path to the real old backend even after the client-facing Service below gets
   redirected to it — reusing the client-facing Service's own name here would otherwise make
   the buffer's outbound writes loop back to itself once its selector changes in step 2),
2. redirects the release's Service at the buffer agent so incoming writes keep flowing,
3. best-effort force-merges the old storage, then takes a CSI VolumeSnapshot of it
   (`-snapshot-class` selects the `VolumeSnapshotClass`; requires the cluster's CSI driver to
   support snapshots),
4. provisions a fresh PVC from that snapshot (`-agent-buffer-size` sizes the buffer agent's
   own persistent queue) and creates the target CR against it,
5. once ready, repoints the buffer agent's output at the target and waits for its queue to
   drain,
6. repoints the release's Service at the target's pods and tears down the buffer agent and
   alias Service.

   The old workload(s) and PVC(s) are left exactly as they were — decommission them yourself
   (e.g. `helm uninstall`) whenever you're ready. If any step from 3 onward fails, the
   Service selector is automatically reverted so traffic keeps flowing through the buffer
   agent rather than being left pointed at a half-finished setup.

For cluster charts (`victoria-metrics-cluster`, `victoria-logs-cluster`), only the
insert component (`vminsert`/`vlinsert`, the write path) goes through the buffer-and-cutover
dance above. The select component (`vmselect`/`vlselect`, reads) has nothing to buffer, so its
Service is cut over directly once the target is ready. The storage component
(`vmstorage`/`vlstorage`) has no client-facing Service at all — insert/select address it
directly — so its per-ordinal PVCs are just force-merged (per pod, since each holds an
independent shard) and CSI-snapshotted, with no Service involved.

Run with `-dry-run` first to print the plan without changing anything. Without `-yes`, it
asks for interactive confirmation before the first destructive/traffic-affecting step.

Currently supports `victoria-metrics-single`, `victoria-logs-single`,
`victoria-metrics-cluster`, and `victoria-logs-cluster` for both `WithDowntime` and
`NoDowntime`.

## Notes

The tool maps the majority of critical parameters, including Replicas, Images, Resource Requests/Limits, Affinity, NodeSelectors, Tolerations, ExtraArgs/ExtraEnvs, PersistentVolumes, and specific behavioral flags. 

## Limitations

Helm converter manifests are not 1:1

Some configurations are currently excluded from automated mapping:
*   **Ingress and ServiceMonitors**: Secondary standalone objects sometimes managed by the Helm charts (like raw `Ingress` objects or default `ServiceMonitor` definitions) are not processed. The Operator typically assumes you manage `Ingress` resources externally or define `VMServiceScrape` logic independently.
*   **Autoscaling and PDBs**: Fields defining `hpa` (HorizontalPodAutoscaler), `vpa` (VerticalPodAutoscaler), and `podDisruptionBudget` are not automatically translated to their embedded Operator CR equivalents at this time. These manifests need to be created manually in case the operator's CR doesn't provide a way to define those.
*   **`fullname` and templating**: Operator doesn't support redefining object names with `fullname`, so resources managed by the operator may have different names.
