---
weight: 6
title: VMCluster
menu:
  docs:
    identifier: operator-cr-vmcluster
    parent: operator-cr
    weight: 6
aliases:
  - /operator/resources/vmcluster/
  - /operator/resources/vmcluster/index.html
tags:
  - kubernetes
  - metrics
---
`VMCluster` represents a high-available and fault-tolerant version of VictoriaMetrics database.
The `VMCluster` CRD defines a [cluster version VM](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/).

For each `VMCluster` resource, the Operator creates:

- `VMStorage` as `StatefulSet`,
- `VMSelect` as `StatefulSet`
- and `VMInsert` as deployment.

For `VMStorage` and `VMSelect` headless services are created. `VMInsert` is created as service with clusterIP.

There is a strict order for these objects creation and reconciliation:

1. `VMStorage` is synced - the Operator waits until all its pods are ready;
1. Then it syncs `VMSelect` with the same manner;
1. `VMInsert` is the last object to sync.

All [statefulsets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) are created
with [OnDelete](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#on-delete) update type.
It allows to manually manage the rolling update process for Operator by deleting pods one by one and waiting for the ready status.

Rolling update process may be configured by the operator env variables.
The most important is `VM_PODWAITREADYTIMEOUT=80s` - it controls how long to wait for pod's ready status.

## Specification

You can see the full actual specification of the `VMCluster` resource in the **[API docs -> VMCluster](https://docs.victoriametrics.com/operator/api/#vmcluster)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vmcluster/#examples) section.

## Services and URLs

For a `VMCluster` named `<name>` in namespace `<namespace>`, the Operator creates the following Kubernetes services:

| Service name | Type | Port | Purpose |
|---|---|---|---|
| `vminsert-<name>` | ClusterIP | 8480 | Metrics ingestion (write path) |
| `vmselect-<name>` | Headless | 8481 | Metrics querying (read path) |
| `vmstorage-<name>` | Headless | 8482 | vmstorage admin HTTP API (not for direct user access) |

VictoriaMetrics cluster uses a **tenant** model where data is namespaced by an account ID.
For single-tenant setups the default tenant is `0`.

Common internal cluster URLs (replace `<name>` and `<namespace>` with your values):

| Use case | URL |
|---|---|
| Prometheus remote write (VMAgent / Prometheus) | `http://vminsert-<name>.<namespace>.svc:8480/insert/0/prometheus/api/v1/write` |
| PromQL / MetricsQL query API | `http://vmselect-<name>.<namespace>.svc:8481/select/0/prometheus` |
| Grafana datasource URL | `http://vmselect-<name>.<namespace>.svc:8481/select/0/prometheus` |
| VMUI (built-in web UI) | `http://vmselect-<name>.<namespace>.svc:8481/select/0/vmui/` |

### Configuring VMAgent to write to VMCluster

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example
  namespace: default
spec:
  selectAllByDefault: true
  remoteWrite:
    - url: "http://vminsert-<name>.default.svc:8480/insert/0/prometheus/api/v1/write"
```

### Customizing service type or port

By default each component service is created with the type and port shown in the table above.
Use `serviceSpec` on each component to change these settings.
Setting `useAsDefault: true` applies the changes to the main service rather than creating an additional one:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example
spec:
  vminsert:
    replicaCount: 2
    serviceSpec:
      useAsDefault: true
      spec:
        type: LoadBalancer
  vmselect:
    replicaCount: 2
    serviceSpec:
      useAsDefault: true
      spec:
        type: LoadBalancer
```

> **Note**: changing `vmselect` or `vmstorage` from headless to a ClusterIP/LoadBalancer type
> may break internal pod-to-pod communication between cluster components.

To expose VMCluster components outside the cluster via an ingress with authentication,
see [Authorization and exposing components — VMCluster](https://docs.victoriametrics.com/operator/auth/#vmcluster).

## Requests Load-Balancing

 Operator provides enhanced load-balancing mechanism for `vminsert` and `vmselect` clients. By default, operator uses built-in Kubernetes [service](https://kubernetes.io/docs/concepts/services-networking/service/) with `clusterIP` type for clients connection. It's good solution for short lived connections. But it acts poorly with long-lived TCP sessions and leads to the uneven resources utilization for `vmselect` and `vminsert` components.

 Consider the following example:

![CR](vmcluster_default_balancer.webp)

 In this case clients could establish multiple connections to the same `pod` via `service`. And client requests will be served only by subset of `pods`.

 Operator allows to tweak this behaviour with enabled [requestsLoadBalancer](https://docs.victoriametrics.com/operator/api/#vmclusterspec-requestsloadbalancer):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: with-balancer
spec:
  retentionPeriod: "4"
  replicationFactor: 1
  vminsert:
    replicaCount: 1
  vmselect:
    replicaCount: 1
  vmstorage:
    replicaCount: 1
  requestsLoadBalancer:
    enabled: true
    spec:
      replicaCount: 2
```

 Operator will deploy `VMAuth` deployment with 2 replicas. And update vminsert and vmselect services to point to `vmauth`.
 In addition, operator will create 3 additional services with the following pattern:

- vminsertinternal-CLUSTER_NAME - needed for vmselect pod discovery
- vmselectinternal-CLUSTER_NAME - needed for vminsert pod discovery
- vmclusterlb-CLUSTER_NAME - needed for metrics collection and exposing `vmselect` and `vminsert` components via `VMAuth` balancer.

 Network scheme with load-balancing:
 ![CR](vmcluster_with_balancer.webp)

The `requestsLoadBalancer` feature works transparently and is managed entirely by the `VMCluster` operator,
with no direct access to the underlying [VMAuth](https://docs.victoriametrics.com/victoriametrics/vmauth/) configuration.
If you need more control over load balancing behavior,
or want to combine request routing with authentication or (m)TLS,
consider deploying a standalone [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth/) resource instead of enabling `requestsLoadBalancer`.

## High availability

The cluster version provides a full set of high availability features - metrics replication, node failover, horizontal scaling.

First, we recommend familiarizing yourself with the high availability tools provided by "VictoriaMetrics Cluster" itself:

- [High availability](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#high-availability),
- [Cluster availability](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#cluster-availability),
- [Replication and data safety](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#replication-and-data-safety).

`VMCluster` supports all listed in the above-mentioned articles parameters and features:

- `replicationFactor` - the number of replicas for each metric.
- for every component of cluster (`vmstorage` / `vmselect` / `vminsert`):
  - `replicaCount` - the number of replicas for components of cluster.
  - `affinity` - the affinity (the pod's scheduling constraints) for components pods. See more details in [kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity).
  - `topologySpreadConstraints` - controls how pods are spread across your cluster among failure-domains such as regions, zones, nodes, and other user-defined topology domains. See more details in [kubernetes docs](https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/).

In addition, operator:

- uses k8s services or vmauth for load balancing between `vminsert` and `vmselect` components,
- uses health checks for to determine the readiness of components for work after restart,
- allows to horizontally scale all cluster components just by changing `replicaCount` field.

Here is an example of a `VMCluster` resource with HA features:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-persistent
spec:
  replicationFactor: 2
  vmstorage:
    replicaCount: 10
    storageDataPath: "/vm-data"
    affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
            - key: "app.kubernetes.io/name"
              operator: In
              values:
              - "vmstorage"
          topologyKey: "kubernetes.io/hostname"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 10Gi
    resources:
      limits:
        cpu: "2"
        memory: 2048Mi
  vmselect:
    replicaCount: 3
    cacheMountPath: "/select-cache"
    affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
            - key: "app.kubernetes.io/name"
              operator: In
              values:
              - "vmselect"
          topologyKey: "kubernetes.io/hostname"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 2Gi
    resources:
      limits:
        cpu: "1"
        memory: "500Mi"
  vminsert:
    replicaCount: 4
    affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
            - key: "app.kubernetes.io/name"
              operator: In
              values:
              - "vminsert"
          topologyKey: "kubernetes.io/hostname"
    resources:
      limits:
        cpu: "1"
        memory: "500Mi"
```

## Version management

For `VMCluster` you can specify tag name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases) and repository setting per cluster object:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example
spec:
  vmstorage:
    replicaCount: 2
    image:
      repository: victoriametrics/vmstorage
      tag: v1.110.13-cluster
      pullPolicy: Always
  vmselect:
    replicaCount: 2
    image:
      repository: victoriametrics/vmselect
      tag: v1.110.13-cluster
      pullPolicy: Always
  vminsert:
    replicaCount: 2
    image:
      repository: victoriametrics/vminsert
      tag: v1.110.13-cluster
      pullPolicy: Always
```

or for all cluster components all together, using `clusterVersion` property:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example
spec:
  clusterVersion: v1.110.13-cluster
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo,
but `imagePullSecrets` is global setting for all `VMCluster` specification:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example
spec:
  vmstorage:
    replicaCount: 2
    image:
      repository: victoriametrics/vmstorage
      tag: v1.110.13-cluster
      pullPolicy: Always
  vmselect:
    replicaCount: 2
    image:
      repository: victoriametrics/vmselect
      tag: v1.110.13-cluster
      pullPolicy: Always
  vminsert:
    replicaCount: 2
    image:
      repository: victoriametrics/vminsert
      tag: v1.110.13-cluster
      pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
  # ...
```

## Update strategy

The Operator provides fine-grained control over how `VMCluster` nodes are restarted when their spec changes (image tag, flags, etc.).
The available strategies map to the [no downtime and minimum downtime upgrade approaches](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#updating--reconfiguring-cluster-nodes)
described in the VictoriaMetrics cluster documentation.

### vmstorage and vmselect

The `rollingUpdateStrategy` field controls how `vmstorage` and `vmselect` nodes are updated:

| Value | Behavior |
|---|---|
| `OnDelete` (default) | Operator restarts nodes one by one and waits for each to become ready before continuing |
| `RollingUpdate` | Kubernetes restarts nodes natively; the Operator only waits for the rollout to complete |

The default `OnDelete` strategy implements the **no downtime** approach: nodes are restarted one at a time
in the order recommended by VictoriaMetrics (`vmstorage` first, then `vmselect`).

Use `rollingUpdateStrategyBehavior.maxUnavailable` to control how many nodes are restarted simultaneously.
The default is `1`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example
spec:
  vmstorage:
    replicaCount: 6
    rollingUpdateStrategyBehavior:
      maxUnavailable: 2        # restart 2 nodes at a time
  vmselect:
    replicaCount: 3
    rollingUpdateStrategyBehavior:
      maxUnavailable: "33%"   # restart up to 33% of nodes at a time
```

Setting `maxUnavailable: "100%"` restarts all nodes in parallel, implementing the **minimum downtime** strategy
at the cost of temporary unavailability:

```yaml
spec:
  vmstorage:
    replicaCount: 3
    rollingUpdateStrategyBehavior:
      maxUnavailable: "100%"
```

To delegate the rolling restart entirely to Kubernetes, set `rollingUpdateStrategy: RollingUpdate`.
In this mode `rollingUpdateStrategyBehavior` is ignored:

```yaml
spec:
  vmstorage:
    replicaCount: 3
    rollingUpdateStrategy: RollingUpdate
```

The timeout for each node to become ready is controlled by the Operator environment variable
`VM_PODWAITREADYTIMEOUT` (default `80s`). The poll interval is `VM_PODWAITREADYINTERVALCHECK` (default `5s`).

### vminsert

`vminsert` nodes can be configured with standard Deployment update parameters:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example
spec:
  vminsert:
    replicaCount: 3
    updateStrategy: RollingUpdate   # default; set to Recreate for minimum downtime
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
```

## Resource management

You can specify resources for each component of `VMCluster` resource in the `spec` section of the `VMCluster` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: resources-example
spec:
    # ...
    vmstorage:
      resources:
          requests:
            memory: "16Gi"
            cpu: "4"
          limits:
            memory: "16Gi"
            cpu: "4"
    # ...
    vmselect:
      resources:
        requests:
          memory: "16Gi"
          cpu: "4"
        limits:
          memory: "16Gi"
          cpu: "4"
    # ...
    vminsert:
      resources:
        requests:
          memory: "16Gi"
          cpu: "4"
        limits:
          memory: "16Gi"
          cpu: "4"
  # ...
```

If these parameters are not specified, then,
by default all `VMCluster` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VMCluster/vmstorage` pods,
- `VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VMCluster/vmstorage` pods,
- `VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VMCluster/vmstorage` pods,
- `VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VMCluster/vmstorage` pods,
- `VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VMCluster/vmselect` pods,
- `VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VMCluster/vmselect` pods.

These default parameters will be used if:

- `VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VMCluster/*` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VMCluster/*` spec have higher priority than operator parameters.

If you set `VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VMCluster/*` CRD,
then `VMCluster/*` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Enterprise features

VMCluster supports following features
from [VictoriaMetrics Enterprise](https://docs.victoriametrics.com/victoriametrics/enterprise/#victoriametrics-enterprise):

- [Downsampling](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#downsampling)
- [Multiple retentions / Retention filters](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#retention-filters)
- [Advanced per-tenant statistic](https://docs.victoriametrics.com/victoriametrics/pertenantstatistic/)
- [mTLS for cluster components](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#mtls-protection)
- [Backup automation](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/)

- [Automatic vmstorage discovery](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#automatic-vmstorage-discovery)

For using Enterprise version of [vmcluster](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/) you need to:
 - specify license at [`spec.license.key`](https://docs.victoriametrics.com/operator/api/#license-key) or at [`spec.license.keyRef`](https://docs.victoriametrics.com/operator/api/#license-keyref).
 - change version of `vmcluster` to version with `-enterprise-cluster` suffix using [Version management](https://docs.victoriametrics.com/operator/resources/vmcluster/#version-management).

### Downsampling

Use `spec.downsampling` to configure [Downsampling](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#downsampling).
The operator automatically applies the rules to both `vmselect` and `vmstorage`. Note that it would overwrite the downsampling configuration set via `extraArgs`
Each rule requires `offset` (how far back to downsample) and `interval` (target resolution).
An optional `filter` restricts the rule to matching time series.
The optional `dedupInterval` sets `-dedup.minScrapeInterval` on both components.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  clusterVersion: v1.110.13-enterprise-cluster
  downsampling:
    dedupInterval: 1m
    rules:
      - periods:
          - offset: 30d
            interval: 5m
          - offset: 180d
            interval: 1h
          - offset: 1y
            interval: 6h
      - filter: '{env="prod"}'
        periods:
          - offset: 30d
            interval: 1m
          - offset: 180d
            interval: 10m

  # ...other fields...
```

You can read more about downsampling configuration on the [VictoriaMetrics cluster downsampling page](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#downsampling).

### Retention filters

Use `spec.vmstorage.retentionFilters` to configure [Retention filters](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#retention-filters) on `vmstorage`. Note that it would overwrite the retention filters configuration set via `extraArgs`
Each entry requires a MetricsQL label `filter` and a `retention` duration.
The global `spec.retentionPeriod` applies to all series that don't match any filter.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  clusterVersion: v1.110.13-enterprise-cluster
  retentionPeriod: "12"
  vmstorage:
    retentionFilters:
      - filter: '{vm_account_id="5",env="dev"}'
        retention: 5d
      - filter: '{vm_account_id="5",env="prod"}'
        retention: 5y

  # ...other fields...
```

You can read more about retention filters configuration on the [VictoriaMetrics cluster retention filters page](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#retention-filters).

### Advanced per-tenant statistic

For using [Advanced per-tenant statistic](https://docs.victoriametrics.com/victoriametrics/pertenantstatistic/)
you only need to [enable Enterprise version of vmcluster components](https://docs.victoriametrics.com/operator/resources/vmcluster/#enterprise-features)
and operator will automatically create
[Scrape objects](https://docs.victoriametrics.com/operator/resources/vmagent/#scraping) for cluster components.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  # enabling enterprise features
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  clusterVersion: v1.110.13-enterprise-cluster

  # ...other fields...
```

After that [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent/) will automatically
scrape [Advanced per-tenant statistic](https://docs.victoriametrics.com/victoriametrics/pertenantstatistic/) for cluster components.

### mTLS protection

You can pass [mTLS protection](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#mtls-protection)
flags to `VMCluster/vmstorage`, `VMCluster/vmselect` and `VMCluster/vminsert` with [extraArgs](https://docs.victoriametrics.com/operator/resources/#extra-arguments) and mount secret files
with `extraVolumes` and `extraVolumeMounts` fields.

Here are complete example for [mTLS protection](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#mtls-protection)

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  # enabling enterprise features
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  clusterVersion: v1.110.13-enterprise-cluster
  vmselect:
    extraArgs:
      # using enterprise features: mTLS protection
      # more details about mTLS protection you can read on https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#mtls-protection
      cluster.tls: true
      cluster.tlsCAFile: /etc/mtls/ca.crt
      cluster.tlsCertFile: /etc/mtls/vmselect.crt
      cluster.tlsKeyFile: /etc/mtls/vmselect.key
    extraVolumes:
      - name: mtls
        secret:
          secretName: mtls
    extraVolumeMounts:
      - name: mtls
        mountPath: /etc/mtls

  vminsert:
    extraArgs:
      # using enterprise features: mTLS protection
      # more details about mTLS protection you can read on https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#mtls-protection
      cluster.tls: true
      cluster.tlsCAFile: /etc/mtls/ca.crt
      cluster.tlsCertFile: /etc/mtls/vminsert.crt
      cluster.tlsKeyFile: /etc/mtls/vminsert.key
    extraVolumes:
      - name: mtls
        secret:
          secretName: mtls
    extraVolumeMounts:
      - name: mtls
        mountPath: /etc/mtls

  vmstorage:
    extraEnvs:
      - name: POD
        valueFrom:
          fieldRef:
            fieldPath: metadata.name
    extraArgs:
      # using enterprise features: mTLS protection
      # more details about mTLS protection you can read on https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#mtls-protection
      cluster.tls: true
      cluster.tlsCAFile: /etc/mtls/ca.crt
      cluster.tlsCertFile: /etc/mtls/$(POD).crt
      cluster.tlsKeyFile: /etc/mtls/$(POD).key
    extraVolumes:
      - name: mtls
        secret:
          secretName: mtls
    extraVolumeMounts:
      - name: mtls
        mountPath: /etc/mtls

  # ...other fields...

---

apiVersion: v1
kind: Secret
metadata:
  name: mtls
  namespace: default
stringData:
  ca.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  mtls-vmstorage-0.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  mtls-vmstorage-0.key: |
    -----BEGIN PRIVATE KEY-----
    ...
    -----END PRIVATE KEY-----
  mtls-vmstorage-1.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  mtls-vmstorage-1.key: |
    -----BEGIN PRIVATE KEY-----
    ...
    -----END PRIVATE KEY-----
  vminsert.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  vminsert.key: |
    -----BEGIN PRIVATE KEY-----
    ...
    -----END PRIVATE KEY-----
  vmselect.crt: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  vmselect.key: |
    -----BEGIN PRIVATE KEY-----
    ...
    -----END PRIVATE KEY-----

```

Example commands for generating certificates you can read
on [this page](https://gist.github.com/f41gh7/76ed8e5fb1ebb9737fe746bae9175ee6#generate-self-signed-ca-with-key).

### Backup automation

You can check [vmbackupmanager documentation](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/) for backup automation.
It contains a description of the service and its features. This section covers vmbackupmanager integration in vmoperator.

`VMCluster` has built-in backup configuration, it uses `vmbackupmanager` - proprietary tool for backups.
It supports incremental backups (hourly, daily, weekly, monthly) with popular object storages (aws s3, google cloud storage).

Here is a complete example for backup configuration:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  vmstorage:
    vmBackup:
      # this feature is only available in Victoriametrics Enterprise
      # more details about backup automation you can read on https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/
      destination: "s3://your_bucket/folder"
      # Read the object storage credentials from a secret
      credentialsSecret:
        name: remote-storage-keys
        key: credentials
      # customS3Endpoint: 'https://s3.example.com' # uncomment and adjust if you using s3 compatible storage instead of AWS s3
      # uncomment and adjust to fit your backup schedule
      # disableHourly: false
      # disableDaily: false
      # disableWeekly: false
      # disableMonthly: false
  # ...other fields...

---

apiVersion: v1
kind: Secret
metadata:
  name: remote-storage-keys
type: Opaque
stringData:
  credentials: |-
    [default]
    aws_access_key_id = your_access_key_id
    aws_secret_access_key = your_secret_access_key
```

**NOTE**: for cluster version operator adds suffix for destination: `"s3://your_bucket/folder"`, it becomes `"s3://your_bucket/folder/$(POD_NAME)"`.
It's needed to make consistent backups for each storage node.

You can read more about backup configuration options and mechanics [here](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/)

Possible configuration options for backup crd can be found at [link](https://docs.victoriametrics.com/operator/api/#vmbackup)

**Using VMBackupmanager for restoring backups** in Kubernetes environment is described [here](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/#how-to-restore-in-kubernetes).

Also see VMCluster example spec [here](https://github.com/VictoriaMetrics/operator/blob/master/config/examples/vmcluster_with_backuper.yaml).

### Automatic vmstorage discovery

By default, the operator statically enumerates all `vmstorage` pod addresses in the `-storageNode` flag of `vminsert` and `vmselect`.
With [automatic vmstorage discovery](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#automatic-vmstorage-discovery),
`vminsert` and `vmselect` resolve storage node addresses dynamically via DNS SRV records, removing the need for a rolling restart when storage nodes scale up or down.

This is an enterprise feature and requires a [valid license key](https://docs.victoriametrics.com/victoriametrics/enterprise/).

The `discovery` field can be set at the cluster level (applies to both `vminsert` and `vmselect`) or overridden per component.

**`spec.discovery` fields:**

| Field      | Description |
|------------|-------------|
| `enabled`  | Enables automatic vmstorage node discovery via DNS SRV records. |
| `interval` | How often to refresh the list of storage nodes. Minimum `1s`, defaults to `2s`. |
| `filter`   | Optional regexp to filter discovered storage addresses. Only matching addresses are used. |

When `discovery` is enabled the operator sets `-storageNode=srv+<headless-service>.<namespace>[.svc.<domain>]:<port>` instead of listing individual pod addresses.
The DNS SRV lookup resolves this to individual pod addresses in the form `<pod-name>.<headless-service>.<namespace>.svc.<domain>:<port>` — for example, `vmstorage-ent-example-0.vmstorage-ent-example.default.svc.cluster.local:8401`.
The `filter` regexp is matched against these full addresses, so it must account for the cluster name embedded in the pod name. The domain suffix does not need to be included in the filter since regexp matching is substring-based.

The `maintenanceInsertNodeIDs` and `maintenanceSelectNodeIDs` fields on `vmstorage` cannot be used together with discovery, since node selection is delegated to the `filter` regexp.

#### Enable discovery globally

This enables discovery with the same settings for both `vminsert` and `vmselect`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  clusterVersion: v1.110.13-enterprise-cluster
  discovery:
    enabled: true
    interval: 5s
  vmstorage:
    replicaCount: 3
  vmselect:
    replicaCount: 2
  vminsert:
    replicaCount: 2
```

#### Override discovery per component

The `discovery` field on `vmselect` or `vminsert` overrides the cluster-level default for that component.
This is useful when you want different refresh intervals or address filters for reads and writes,
or when you want to disable discovery for one component while keeping it enabled globally:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: ent-example
spec:
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  clusterVersion: v1.110.13-enterprise-cluster
  # global default: discovery enabled for both components
  discovery:
    enabled: true
    interval: 5s
  vmstorage:
    replicaCount: 6
  vmselect:
    replicaCount: 2
    # override: read only from nodes 0-2 (pod name format: vmstorage-ent-example-N)
    discovery:
      enabled: true
      interval: 10s
      filter: "vmstorage-ent-example-[0-2]\\."
  vminsert:
    replicaCount: 2
    # override: disable discovery for vminsert, use static addresses instead
    discovery:
      enabled: false
```

## Examples

### Minimal example without persistence

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-minimal
spec:
  # ...
  retentionPeriod: "1"
  vmstorage:
    replicaCount: 2
  vmselect:
    replicaCount: 2
  vminsert:
    replicaCount: 2
```

### With persistence

```yaml
kind: VMCluster
metadata:
  name: example-persistent
spec:
  # ...
  retentionPeriod: "4"
  replicationFactor: 2
  vmstorage:
    replicaCount: 2
    storageDataPath: "/vm-data"
    storage:
      volumeClaimTemplate:
        spec:
          storageClassName: standard
          resources:
            requests:
              storage: 10Gi
    resources:
      limits:
        cpu: "0.5"
        memory: 500Mi
  vmselect:
    replicaCount: 2
    cacheMountPath: "/select-cache"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 2Gi
    resources:
      limits:
        cpu: "0.3"
        memory: "300Mi"
  vminsert:
    replicaCount: 2
```
