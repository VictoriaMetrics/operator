---
weight: 21
title: VLCluster
menu:
  docs:
    identifier: operator-cr-vlcluster
    parent: operator-cr
    weight: 21
aliases:
  - /operator/resources/vlcluster/
  - /operator/resources/vlcluster/index.html
tags:
  - kubernetes
  - metrics
---
`VLCluster` represents database for storing logs {{% available_from "v0.59.0" "operator" %}}.
The `VLCluster` CRD declaratively defines a [VictoriaLogs cluster](https://docs.victoriametrics.com/victorialogs/cluster/)
installation to run in a Kubernetes cluster.

For each `VLCluster` resource, the Operator deploys a properly configured components `VLInsert`'s `Deployment`, `VLSelect`'s `Deployment` and `VLStorage`'s `StatefulSet` in the same namespace.
The VLStorage `Pod`s are configured to mount an empty dir or `StorageSpec` for storing data.

For each `VLCluster` resource components, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VLCluster.metadata.name`.

## Specification

You can see the full actual specification of the `VLCluster` resource in the **[API docs -> VLCluster](https://docs.victoriametrics.com/operator/api/#vlcluster)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vlcluster/#examples) section.

## Services and URLs

For a `VLCluster` named `<name>` in namespace `<namespace>`, the Operator creates the following Kubernetes services:

| Service name | Type | Port | Purpose |
|---|---|---|---|
| `vlinsert-<name>` | ClusterIP | 9481 | Log ingestion (write path) |
| `vlselect-<name>` | Headless | 9471 | Log querying (read path) |
| `vlstorage-<name>` | Headless | 9491 | vlstorage admin HTTP API (not for direct user access) |

Common internal cluster URLs (replace `<name>` and `<namespace>` with your values):

| Use case | URL |
|---|---|
| VLAgent remoteWrite | `http://vlinsert-<name>.<namespace>.svc:9481/internal/insert` |
| LogsQL query API | `http://vlselect-<name>.<namespace>.svc:9471/select/logsql/query` |
| Grafana datasource URL | `http://vlselect-<name>.<namespace>.svc:9471/select` |
| VMUI (built-in web UI) | `http://vlselect-<name>.<namespace>.svc:9471/select/vmui/` |

`vlinsert` also accepts logs from all [data ingestion protocols](https://docs.victoriametrics.com/victorialogs/data-ingestion/) supported by VictoriaLogs
(Elasticsearch, Loki, OpenTelemetry, Syslog, etc.) directly at port `9481`.

### Configuring VLAgent to write to VLCluster

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: example
  namespace: default
spec:
  remoteWrite:
    - url: "http://vlinsert-<name>.default.svc:9481/internal/insert"
```

### Customizing service type or port

By default each component service is created with the type and port shown in the table above.
Use `serviceSpec` on each component to change these settings.
Setting `useAsDefault: true` applies the changes to the main service rather than creating an additional one:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: example
spec:
  vlinsert:
    replicaCount: 2
    serviceSpec:
      useAsDefault: true
      spec:
        type: LoadBalancer
  vlselect:
    replicaCount: 2
    serviceSpec:
      useAsDefault: true
      spec:
        type: LoadBalancer
```

> **Note**: changing `vlselect` or `vlstorage` from headless to a ClusterIP/LoadBalancer type
> may break internal pod-to-pod communication between cluster components.

To expose VLCluster components outside the cluster via an ingress with authentication,
see [Authorization and exposing components — VLCluster](https://docs.victoriametrics.com/operator/auth/#vlcluster).

## Version management

To set `VLCluster` version add `spec.clusterVersion` or `spec.COMPONENT.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaLogs/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: example
spec:
  ...
  clusterVersion: v1.26.0
  vlstorage:
    image:
      repository: victoriametrics/victoria-logs
      tag: v1.25.0
      pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: example
spec:
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Update strategy

The Operator provides fine-grained control over how `VLCluster` nodes are restarted when their spec changes (image tag, flags, etc.).

### vlstorage

`vlstorage` nodes are restarted one by one by default.
The `rollingUpdateStrategy` field controls this behavior:

| Value | Behavior |
|---|---|
| `OnDelete` (default) | Operator restarts nodes one by one and waits for each to become ready before continuing |
| `RollingUpdate` | Kubernetes restarts nodes natively; the Operator only waits for the rollout to complete |

Use `rollingUpdateStrategyBehavior.maxUnavailable` to control how many nodes are restarted simultaneously (default is `1`):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: example
spec:
  vlstorage:
    replicaCount: 6
    rollingUpdateStrategy: OnDelete
    rollingUpdateStrategyBehavior:
      maxUnavailable: 2        # restart 2 nodes at a time
```

Setting `maxUnavailable: "100%"` restarts all `vlstorage` nodes in parallel, minimizing total upgrade time
at the cost of temporary unavailability.

The timeout for each node to become ready is controlled by the Operator environment variable
`VM_PODWAITREADYTIMEOUT` (default `80s`). The poll interval is `VM_PODWAITREADYINTERVALCHECK` (default `5s`).

### vlinsert and vlselect

`vlinsert` and `vlselect` nodes support standard Deployment update parameters:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: example
spec:
  vlinsert:
    replicaCount: 2
    updateStrategy: RollingUpdate   # default; set to Recreate to stop all nodes before starting new ones
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  vlselect:
    replicaCount: 2
    updateStrategy: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
```

## Resource management

You can specify resources for each `VLCluster` resource components in the `spec` section of the `VLCluster` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: resources
spec:
    # ...
    vlstorage:
      resources:
          requests:
            memory: "64Mi"
            cpu: "250m"
          limits:
            memory: "128Mi"
            cpu: "500m"
    # ...
```

If these parameters are not specified, then,
by default all `VLCluster` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VLCluster.vlstorage` pods,
- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_LIMIT_CPU` - default cpu limit for `VLCluster.vlstorage` pods,
- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VLCluster.vlstorage` pods,
- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_CPU` - default cpu limit for `VLCluster.vlstorage` pods.

These default parameters will be used if:

- `VM_VLCLUSTERDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VLCluster` CR doesn't have `resources` field in `spec` section for component.

Field `resources` in `VLSingle` spec have higher priority than operator parameters.

If you set `VM_VLCLUSTEREDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VLSingle` CRD,
then `VLSingle` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Requests Load-Balancing

 Operator provides enhanced load-balancing mechanism for `vlinsert` and `vlselect` clients. By default, operator uses built-in Kubernetes [service](https://kubernetes.io/docs/concepts/services-networking/service/) with `clusterIP` type for clients connection. It's good solution for short lived connections. But it acts poorly with long-lived TCP sessions and leads to the uneven resources utilization for `vlselect` and `vlinsert` components.

 Operator allows to tweak Kubernetes TCP-based load-balancing with enabled [requestsLoadBalancer](https://docs.victoriametrics.com/operator/api/#vmclusterspec-requestsloadbalancer):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: with-balancer
spec:
  vlinsert:
    replicaCount: 2
  vlselect:
    replicaCount: 2
  vlstorage:
    retentionPeriod: "1y"
    replicaCount: 2
  requestsLoadBalancer:
    enabled: true
    spec:
     replicaCount: 2
```

 Operator will deploy `VMAuth` deployment with 2 replicas. And update vlinsert and vlselect services to point to `vmauth`.
 In addition, operator will create 3 additional services with the following pattern:

- vlinsertint-CLUSTER_NAME - needed for vlselect pod discovery
- vlselectint-CLUSTER_NAME - needed for vlinsert pod discovery
- vlclusterlb-CLUSTER_NAME - needed for metrics collection and exposing `vlselect` and `vlinsert` components via `VMAuth` balancer.

The `requestsLoadBalancer` feature works transparently and is managed entirely by the `VLCluster` operator,
with no direct access to the underlying [VMAuth](https://docs.victoriametrics.com/victoriametrics/vmauth/) configuration.
If you need more control over load balancing behavior,
or want to combine request routing with authentication or (m)TLS,
consider deploying a standalone [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth/) resource instead of enabling `requestsLoadBalancer`.


## Examples

```yaml

apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: sample
spec:
  vlinsert:
    replicaCount: 1
  vlselect:
    replicaCount: 1
  vlstorage:
    retentionPeriod: "1y"
    replicaCount: 2
```
