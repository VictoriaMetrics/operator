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
standard Helm labels, then runs one of two strategies (`-strategy`). Both strategies buffer
every write made over a *new* connection through a VMAgent (VLAgent for VictoriaLogs) before
touching anything, so those are never lost under either strategy — the main thing that differs
between them is whether *reads* stay available during the migration window. There's one
narrower write-loss caveat that also differs by strategy — a client with a connection already
established before the cutover — covered where each strategy's steps are listed below:

**`WithDowntime`** (default) — reads are unavailable for the whole migration window, since old
storage (and, for cluster charts, select) is deleted rather than kept running:

1. deploys a buffering VMAgent pointed directly at the target's future write endpoint —
   deterministic from the target's name, so this works before the target CR even exists — and
   redirects the release's Service (cluster charts: just the insert Service) to it, so incoming
   writes keep flowing into the agent's persistent queue,
2. deletes the old Deployment,
3. rebinds the existing PersistentVolume under the operator's own PVC name — no data is
   copied, the same volume is reused,
4. creates the target CR and waits for it to become ready, then deletes any ConfigMap/Secret
   the old pod spec actually referenced,
5. repoints the release's Service at the new pods, preserving the Service's name and DNS
   entry, then waits for the buffer agent's queue to drain and tears it down.

A client with a connection already established to the old workload before step 1's redirect —
directly, or routed through the Service, since a selector change only affects new connections —
can keep writing to it afterward. Unlike `NoDowntime` below, that write generally isn't lost: the
old PersistentVolume is rebound (not snapshotted) under the target, so a write that lands before
the old pod has actually terminated ends up on the same volume the target now uses. Only a write
attempted after that pod is fully gone fails outright, visibly, at the client.

For cluster charts (`victoria-metrics-cluster`, `victoria-logs-cluster`), the same steps run
once per component (`vmstorage`/`vmselect`/`vminsert`, or `vlstorage`/`vlselect`/`vlinsert`),
discovered via the chart's own `app.kubernetes.io/component` label: each component's old
StatefulSet/Deployment is deleted, each StatefulSet component's PVCs are rebound one per
ordinal (`vmstorage`/`vmselect`, or `vlstorage` — the insert/select-without-cache components
have no persistent storage), then the single target VMCluster/VLCluster CR is created once and
every component's Service is repointed after it becomes ready — insert's Service comes back
from the buffer agent, the rest from their own old pods.

**`NoDowntime`** — never touches the old workload(s) or their PVC(s). For cluster charts, only
the insert Service is redirected to the buffer, so reads against the select Service stay
available for (almost) the whole window too. For single-node charts there's no such split: the
release has just one Service serving both reads and writes, so redirecting it to
the write-only buffer agent in step 2 below means reads against it fail from that point until
the final cutover in step 5 — a read-only alias to the old backend covers the rest of the
migration window, but single-node charts don't have cluster charts' continuous read path via a
separate component. The alias is a stopgap, not a seamless read path: it has a temporary,
migration-specific name, so read clients still need to be manually repointed at it — and then
off it again before the final cutover (step 5), since `migrate` deletes the alias Service itself
as soon as the release's own Service starts serving both reads and writes again; a client still
pointed at the alias when that happens loses connectivity immediately, with no grace period. The
`victoria-metrics-single`/`victoria-logs-single`
Helm charts' `http` value lets the workload listen on multiple addresses, but every listener
still lands on the same one Service (just a different port), so it doesn't give reads a Service
of their own. The chart's generic `extraObjects` escape hatch provides one: add a second
Service, pointed at the same pod labels as the release's own Service, but *without* that
release's own `app.kubernetes.io/instance`/`app.kubernetes.io/managed-by: Helm` labels —
`migrate` discovers Services by exactly those labels and expects to find one, so a second
Service carrying them would either get swept into the migration or make discovery fail
outright.

```yaml
# values.yaml, alongside the chart's own settings
extraObjects:
  - apiVersion: v1
    kind: Service
    metadata:
      name: my-release-reads
      # deliberately no app.kubernetes.io/instance or app.kubernetes.io/managed-by: Helm here
    spec:
      selector: # copy this from: kubectl get deploy -l app.kubernetes.io/instance=my-release -o jsonpath='{.items[0].spec.selector.matchLabels}'
        app.kubernetes.io/name: victoria-metrics-single # or victoria-logs-single, matching the installed chart
        app.kubernetes.io/instance: my-release
      ports:
        - name: http
          port: 8428
          targetPort: http
          protocol: TCP
```

Point read-only clients at that stable Service *before* running `migrate`, and this whole
alias dance becomes unnecessary — reads are already decoupled from the Service `migrate`
redirects. Once the target CR exists, it also supports its own second, permanent Service via
`spec.serviceSpec` (`useAsDefault: false`, the default) — worth configuring there too if you
want the same on the operator-managed side going forward; see the [`AdditionalServiceSpec`
reference](https://docs.victoriametrics.com/operator/api/#additionalservicespec). The steps are:

1. deploys a buffering VMAgent (VLAgent for VictoriaLogs) pointed directly at the target's
   future write endpoint — deterministic from the target's name, so this works before the
   target CR even exists. Its persistent queue absorbs every write from step 2 onward until
   the target is ready, so there's no separate "repoint the buffer" step, and old storage's
   freshness at snapshot time (step 3) doesn't affect correctness,
2. redirects the release's Service to the buffer agent so incoming writes keep flowing,
3. best-effort force-merges the old storage, then takes a CSI VolumeSnapshot of it
   (`-snapshot-class` selects the `VolumeSnapshotClass`; requires the cluster's CSI driver to
   support snapshots),
4. provisions a fresh PVC from that snapshot and creates the target CR against it
   (`-agent-buffer-size` sizes the buffer agent's own persistent queue),
5. once ready, repoints the release's Service at the target's pods and waits for the buffer
   agent's queue to drain, then tears it down.

   The old workload(s) and PVC(s) are left exactly as they were, but the release's Service(s)
   now point at the target — and they're still tracked by the Helm release, so `helm uninstall`
   deletes them along with everything else, taking down whichever endpoint clients are still
   using them through. Move clients to the target's own Service(s) first, then decommission the
   release once nothing depends on the old Service(s) anymore. There's no automatic rollback:
   once step 2's redirect to the buffer agent succeeds, a later failure (snapshot, target
   creation, final cutover) leaves traffic on the buffer agent rather than reverting, since
   reverting could split-brain the data (everything already buffered lives only in the agent's
   queue and, eventually, the target) — instead the command reports a resumable error and
   re-running it picks up where it left off; writes are safe on disk throughout.

   A client with a connection already established to the old workload before that first
   cutover — directly, or routed through the Service, since a selector change only affects new
   connections — can keep writing to it afterward, for cluster charts' insert component just as
   much as for single-node charts (neither is scaled down or deleted; both are left running
   until you decommission the release). Unlike `WithDowntime` above, that write *is* at risk of
   being lost here: it's captured only if it lands before the storage force-merge/snapshot in
   step 3 — an unavoidable limit of any Service-based cutover while the old workload keeps
   running.

For cluster charts (`victoria-metrics-cluster`, `victoria-logs-cluster`), only the
insert component (`vminsert`/`vlinsert`, the write path) goes through the buffer-and-cutover
dance above. The select component (`vmselect`/`vlselect`, reads) has nothing to buffer, so its
Service is cut over directly once the target is ready. The storage component
(`vmstorage`/`vlstorage`) has no client-facing Service at all — insert/select address it
directly — so its per-ordinal PVCs are just force-merged (per pod, since each holds an
independent shard) and CSI-snapshotted, with no Service involved.

Both strategies route writes through the buffer agent, which only forwards over plain HTTP —
`migrate` refuses to run at all against a write endpoint (single-node's Deployment, or
cluster's insert component) that has `-tls` enabled, for either `WithDowntime` or `NoDowntime`.

Run with `-dry-run` first to print the plan without changing anything. Without `-yes`, it
asks for interactive confirmation before the first destructive/traffic-affecting step.

Currently supports `victoria-metrics-single`, `victoria-logs-single`,
`victoria-metrics-cluster`, and `victoria-logs-cluster` for both `WithDowntime` and
`NoDowntime`. `victoria-traces-single` and `victoria-traces-cluster` aren't supported by
`migrate` — VictoriaTraces has no buffering agent CRD yet, and buffering writes is a hard
requirement for both strategies — though they're still supported by the offline `convert`
command above.

## Notes

The tool maps the majority of critical parameters, including Replicas, Images, Resource Requests/Limits, Affinity, NodeSelectors, Tolerations, ExtraArgs/ExtraEnvs, PersistentVolumes, and specific behavioral flags. 

For `victoria-metrics-auth`, the chart's `config` value (vmauth's own native config file) is parsed too: each entry under `config.users` becomes a standalone `VMUser` CR, and `config.unauthorized_user` becomes the `VMAuth` CR's `spec.unauthorizedUserAccessSpec`. Since a single output file can now contain multiple CRs, the generated `VMUser` manifests are appended after the `VMAuth` one as additional YAML documents (separated by `---`). The `VMAuth` CR's `spec.userSelector` is set to match a label applied to each generated `VMUser`, so the operator actually loads them — without it, a `VMAuth`'s default selectors match nothing.

## Limitations

Helm converter manifests are not 1:1

Some configurations are currently excluded from automated mapping:
*   **Ingress and ServiceMonitors**: Secondary standalone objects sometimes managed by the Helm charts (like raw `Ingress` objects or default `ServiceMonitor` definitions) are not processed. The Operator typically assumes you manage `Ingress` resources externally or define `VMServiceScrape` logic independently.
*   **Autoscaling and PDBs**: Fields defining `hpa` (HorizontalPodAutoscaler), `vpa` (VerticalPodAutoscaler), and `podDisruptionBudget` are not automatically translated to their embedded Operator CR equivalents at this time. These manifests need to be created manually in case the operator's CR doesn't provide a way to define those.
*   **`fullname` and templating**: Operator doesn't support redefining object names with `fullname`, so resources managed by the operator may have different names.
