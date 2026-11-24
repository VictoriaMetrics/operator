---
weight: 25
title: VMEstimator
menu:
  docs:
    identifier: operator-cr-vmestimator
    parent: operator-cr
    weight: 25
aliases:
  - /operator/resources/vmestimator/
  - /operator/resources/vmestimator/index.html
tags:
  - kubernetes
  - metrics
  - cardinality
---
`VMEstimator` represents [vmestimator](https://docs.victoriametrics.com/victoriametrics/vmestimator/) - a real-time cardinality estimator
for metrics ingested via [Prometheus remote write protocol](https://prometheus.io/docs/specs/prw/remote_write_spec/).

The `VMEstimator` CRD declaratively defines a vmestimator installation to run in a Kubernetes cluster.
It consists of the following components:

* **storage** - always deployed as a `StatefulSet` `vmestimator-storage-<name>` with `spec.storage.replicaCount` replicas (`1` by default).
  Storage nodes accept remote write requests and maintain cardinality estimations.
* **select** - optional, deployed as a `Deployment` `vmestimator-select-<name>` if `spec.select` is set.
  Select nodes query all storage nodes, merge their estimations and expose them as metrics.
  See [cluster](https://docs.victoriametrics.com/victoriametrics/vmestimator/#cluster) for details.

A single storage node without select is enough for most setups.
Add `spec.select` if storage is scaled to several replicas, since each storage node has estimations only for the data it received.
If `spec.select` is removed from the spec, the Operator removes all select objects.

Objects of each component are prefixed with `vmestimator-<component>-` and the name from `VMEstimator.metadata.name`,
while shared objects, such as configuration `ConfigMap` and `ServiceAccount`, are named `vmestimator-<name>`.
Pods of a `StatefulSet` with a long name cannot be created, so the object name must not exceed 32 characters.

## Specification

You can see the full actual specification of the `VMEstimator` resource in the **[API docs -> VMEstimator](https://docs.victoriametrics.com/operator/api/#v1-vmestimator)**.

For more information on additional arguments see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vmestimator/#examples) section.

## Configuration

vmestimator computes cardinality according to the configured [streams](https://docs.victoriametrics.com/victoriametrics/vmestimator/#configuration).
Streams can be defined at `spec.streams` or loaded from a `ConfigMap` key referenced at `spec.streamsConfigMap`,
exactly one of them must be set.
The `ConfigMap` key must contain vmestimator configuration in YAML format with a top-level `streams` list,
see [example config](https://github.com/VictoriaMetrics/vmestimator/blob/main/streams.yaml).

The Operator validates streams, generates the configuration file and stores it at the `vmestimator-<name>` `ConfigMap`,
which is mounted to storage pods.
Configuration changes are applied without pods restart: the `config-reloader` sidecar container watches the mounted `ConfigMap`
and triggers configuration reload of vmestimator.
The sidecar can be configured with `configReloaderImage`, `configReloaderResources`, `configReloaderExtraArgs`
and `configReloadAuthKeySecret` fields at `spec.storage`.
Note, that changes of the `ConfigMap` referenced at `spec.streamsConfigMap` are picked up during the next periodic reconciliation,
which is configured with [VM_FORCERESYNCINTERVAL](https://docs.victoriametrics.com/operator/configuration/#variables-vm-forceresyncinterval).

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: example
spec:
  streams:
    # global cardinality and churn ratio
    - interval: 15m
      calculate_churn: true
    # cardinality and churn ratio per job
    - interval: 15m
      calculate_churn: true
      group_by: [job]
    # number of unique values per label name, excluding dev environment
    - interval: 15m
      filter: '{env!="dev"}'
      group_by: [__label__]
      labels:
        cluster: prod
```

Command-line flags, which aren't covered by the spec, for example `deduplication.interval` or `cardinalityMetrics.minCardinality`,
can be set via `extraArgs` of the corresponding component.
See the [list of command-line flags](https://docs.victoriametrics.com/victoriametrics/vmestimator/#command-line-flags).

## Services and URLs

For a `VMEstimator` named `<name>` in namespace `<namespace>`, the Operator creates the following Kubernetes services:

| Service name | Created | Type | Port | Purpose |
|---|---|---|---|---|
| `vmestimator-storage-<name>` | always | Headless | 8490 | Remote write ingestion and stable network identities of storage nodes, used by select nodes |
| `vmestimator-select-<name>` | if `spec.select` is set | ClusterIP | 8490 | Cardinality estimations merged from all storage nodes |

Remote write URL: `http://vmestimator-storage-<name>.<namespace>.svc:8490/cardinality/api/v1/write`.

Cardinality estimations are exposed at `/metrics` path:

* without select - by storage nodes at `http://vmestimator-storage-<name>.<namespace>.svc:8490/metrics`,
* with select - by select nodes at `http://vmestimator-select-<name>.<namespace>.svc:8490/metrics`.
  In this case storage nodes expose local estimations at `/cardinality/metrics` path,
  so their `/metrics` path contains only operational metrics.
  This could be changed with `cardinalityMetrics.exposeAt` flag at `spec.storage.extraArgs`.

Select with `spec.select.replicaCount: 0` is treated as disabled, so storage nodes expose estimations at `/metrics` path.

## Network policy

Each component supports `networkPolicy` field. When it's set, the Operator creates a `NetworkPolicy` for the component pods
with the given `ingress` and `egress` rules, and removes it when the field is removed.
For example, allow traffic only from VMAgent pods:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: example
spec:
  streams:
    - interval: 5m
  storage:
    networkPolicy:
      ingress:
        - from:
            - podSelector:
                matchLabels:
                  app.kubernetes.io/name: vmagent
```

## Sending data

It's recommended to [replicate](https://docs.victoriametrics.com/victoriametrics/vmagent/#replication-and-high-availability)
all the metrics from [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent/) into vmestimator
and to disable on-disk queue for vmestimator, so it cannot affect the main ingestion pipeline:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example
spec:
  selectAllByDefault: true
  remoteWrite:
    - url: "http://vmsingle-example.default.svc:8428/api/v1/write"
    - url: "http://vmestimator-storage-example.default.svc:8490/cardinality/api/v1/write"
  extraArgs:
    # values are applied to remoteWrite urls in the order of their definition
    remoteWrite.disableOnDiskQueue: "false,true"
```

## Scraping cardinality metrics

By default, the Operator creates `VMServiceScrape` for each component, so cardinality estimations
are collected by [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent/) together with operational metrics.
Use `disableSelfServiceScrape` and `serviceScrapeSpec` fields of the components to customize it if needed.

See [alerting rules](https://docs.victoriametrics.com/victoriametrics/vmestimator/#alerting)
and [dashboards](https://docs.victoriametrics.com/victoriametrics/vmestimator/#dashboards) for vmestimator.

## High availability

A single storage node doesn't provide high availability. Use several storage replicas with select instead,
see [cluster](https://docs.victoriametrics.com/victoriametrics/vmestimator/#cluster)
and [Services and URLs](https://docs.victoriametrics.com/operator/resources/vmestimator/#services-and-urls) for load-balancing remote write requests.

## Version management

To set `VMEstimator` version add `spec.componentVersion` or `spec.COMPONENT.image.tag` name from [releases](https://github.com/VictoriaMetrics/vmestimator/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: example
spec:
  componentVersion: v0.1.18
  storage:
    image:
      repository: victoriametrics/vmestimator
      tag: v0.1.18
      pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: example
spec:
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Resource management

You can specify resources for each component of the `VMEstimator` resource in the `spec` section of the `VMEstimator` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: resources
spec:
  # ...
  storage:
    resources:
      requests:
        memory: "256Mi"
        cpu: "100m"
      limits:
        memory: "1Gi"
        cpu: "1"
  # ...
```

If these parameters are not specified, then,
by default all `VMEstimator` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VMESTIMATORDEFAULT_STORAGE_RESOURCE_LIMIT_MEM` - default memory limit for `VMEstimator.storage` pods,
- `VM_VMESTIMATORDEFAULT_STORAGE_RESOURCE_LIMIT_CPU` - default cpu limit for `VMEstimator.storage` pods,
- `VM_VMESTIMATORDEFAULT_STORAGE_RESOURCE_REQUEST_MEM` - default memory request for `VMEstimator.storage` pods,
- `VM_VMESTIMATORDEFAULT_STORAGE_RESOURCE_REQUEST_CPU` - default cpu request for `VMEstimator.storage` pods.

The same parameters with `SELECT` instead of `STORAGE` are used for `VMEstimator.select` pods.

These default parameters will be used if:

- `VM_VMESTIMATORDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VMEstimator` CR doesn't have `resources` field in `spec` section for component.

Field `resources` in `VMEstimator` spec has higher priority than operator parameters.

If you set `VM_VMESTIMATORDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VMEstimator` CRD,
then `VMEstimator` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Examples

Single storage node:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: example
spec:
  streams:
    - interval: 5m
    - interval: 5m
      group_by: [job]
    - interval: 5m
      group_by: [__name__]
  storage:
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        memory: 1Gi
```

Several storage nodes with select:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMEstimator
metadata:
  name: example
spec:
  streamsConfigMap:
    name: vmestimator-streams
    key: streams.yaml
  storage:
    replicaCount: 3
    podDisruptionBudget:
      maxUnavailable: 1
    serviceSpec:
      metadata:
        name: vmestimator-insert-example
      spec: {}
  select:
    replicaCount: 2
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: vmestimator-streams
data:
  streams.yaml: |
    streams:
      - interval: '15m'
        calculate_churn: true
      - interval: '15m'
        calculate_churn: true
        group_by: ['job']
```
