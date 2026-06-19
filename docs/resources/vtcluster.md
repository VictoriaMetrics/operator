---
weight: 24
title: VTCluster
menu:
  docs:
    identifier: operator-cr-vtcluster
    parent: operator-cr
    weight: 22
aliases:
  - /operator/resources/vtcluster/
  - /operator/resources/vtcluster/index.html
tags:
  - kubernetes
  - metrics
---
`VTCluster` represents database for storing traces {{% available_from "v0.62.0" "operator" %}}.
The `VTCluster` CRD declaratively defines a [VictoriaTraces cluster](https://docs.victoriametrics.com/victoriatraces/)
installation to run in a Kubernetes cluster.

For each `VTCluster` resource, the Operator deploys a properly configured components `VTInsert`'s `Deployment`, `VTSelect`'s `Deployment` and `VTStorage`'s `StatefulSet` in the same namespace.
The VTStorage `Pod`s are configured to mount an empty dir or `StorageSpec` for storing data.

For each `VTCluster` resource components, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VTCluster.metadata.name`.

## Specification

You can see the full actual specification of the `VTCluster` resource in the **[API docs -> VTCluster](https://docs.victoriametrics.com/operator/api/#vtcluster)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vtcluster/#examples) section.

## Services and URLs

For a `VTCluster` named `<name>` in namespace `<namespace>`, the Operator creates the following Kubernetes services:

| Service name | Type | Port | Purpose |
|---|---|---|---|
| `vtinsert-<name>` | ClusterIP | 10481 | Trace ingestion via OTLP (write path) |
| `vtselect-<name>` | Headless | 10471 | Trace querying (read path) |
| `vtstorage-<name>` | Headless | 10491 | vtstorage admin HTTP API (not for direct user access) |

Common internal cluster URLs (replace `<name>` and `<namespace>` with your values):

| Use case | URL |
|---|---|
| OTLP/HTTP trace ingestion | `http://vtinsert-<name>.<namespace>.svc:10481/insert/opentelemetry/v1/traces` |
| Query API | `http://vtselect-<name>.<namespace>.svc:10471/select/...` |
| VMUI (built-in web UI) | `http://vtselect-<name>.<namespace>.svc:10471/select/vmui/` |

`vtinsert` accepts trace spans via the [OpenTelemetry protocol (OTLP)](https://opentelemetry.io/docs/specs/otlp/) over HTTP and gRPC.
See [data ingestion docs](https://docs.victoriametrics.com/victoriatraces/data-ingestion/opentelemetry/) for full configuration details.

### Customizing service type or port

By default each component service is created with the type and port shown in the table above.
Use `serviceSpec` on each component to change these settings.
Setting `useAsDefault: true` applies the changes to the main service rather than creating an additional one:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: example
spec:
  insert:
    replicaCount: 2
    serviceSpec:
      useAsDefault: true
      spec:
        type: LoadBalancer
  select:
    replicaCount: 2
    serviceSpec:
      useAsDefault: true
      spec:
        type: LoadBalancer
```

> **Note**: changing `select` or `storage` from headless to a ClusterIP/LoadBalancer type
> may break internal pod-to-pod communication between cluster components.

To expose VTCluster components outside the cluster via an ingress with authentication,
see [Authorization and exposing components — VTCluster](https://docs.victoriametrics.com/operator/auth/#vtcluster).

## Version management

To set `VTCluster` version add `spec.clusterVersion` or `spec.COMPONENT.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaTraces/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: example
spec:
  ...
  clusterVersion: v0.1.0
  storage:
    image:
      repository: victoriametrics/victoria-traces
      tag: v0.1.0
      pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: example
spec:
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Update strategy

The Operator provides fine-grained control over how `VTCluster` nodes are restarted when their spec changes (image tag, flags, etc.).

### storage

`storage` nodes are restarted one by one by default.
The `rollingUpdateStrategy` field controls this behavior:

| Value | Behavior |
|---|---|
| `OnDelete` (default) | Operator restarts nodes one by one and waits for each to become ready before continuing |
| `RollingUpdate` | Kubernetes restarts nodes natively; the Operator only waits for the rollout to complete |

Use `rollingUpdateStrategyBehavior.maxUnavailable` to control how many nodes are restarted simultaneously (default is `1`):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: example
spec:
  storage:
    replicaCount: 6
    rollingUpdateStrategy: OnDelete
    rollingUpdateStrategyBehavior:
      maxUnavailable: 2        # restart 2 nodes at a time
```

Setting `maxUnavailable: "100%"` restarts all `storage` nodes in parallel, minimizing total upgrade time
at the cost of temporary unavailability.

The timeout for each node to become ready is controlled by the Operator environment variable
`VM_PODWAITREADYTIMEOUT` (default `80s`). The poll interval is `VM_PODWAITREADYINTERVALCHECK` (default `5s`).

### insert and select

`insert` and `select` nodes support standard Deployment update parameters:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: example
spec:
  insert:
    replicaCount: 2
    updateStrategy: RollingUpdate   # default; set to Recreate to stop all nodes before starting new ones
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  select:
    replicaCount: 2
    updateStrategy: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
```

## Resource management

You can specify resources for each `VTCluster` resource components in the `spec` section of the `VTCluster` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: resources
spec:
    # ...
    storage:
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
by default all `VTCluster` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_LIMIT_MEM` - default memory limit for `VTCluster.storage` pods,
- `VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_LIMIT_CPU` - default cpu limit for `VTCluster.vtstorage` pods,
- `VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_REQUEST_MEM` - default memory limit for `VTCluster.storage` pods,
- `VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_REQUEST_CPU` - default cpu limit for `VTCluster.storage` pods.

These default parameters will be used if:

- `VM_VTCLUSTERDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VTCluster` CR doesn't have `resources` field in `spec` section for component.

Field `resources` in `VTSingle` spec have higher priority than operator parameters.

If you set `VM_VTCLUSTERDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VTSingle` CRD,
then `VTSingle` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Requests Load-Balancing

 Operator provides enhanced load-balancing mechanism for `insert` and `select` clients. By default, operator uses built-in Kubernetes [service](https://kubernetes.io/docs/concepts/services-networking/service/) with `clusterIP` type for clients connection. It's good solution for short lived connections. But it acts poorly with long-lived TCP sessions and leads to the uneven resources utilization for `select` and `insert` components.

 Operator allows to tweak Kubernetes TCP-based load-balancing with enabled [requestsLoadBalancer](https://docs.victoriametrics.com/operator/api/#vmclusterspec-requestsloadbalancer):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: with-balancer
spec:
  insert:
    replicaCount: 2
  select:
    replicaCount: 2
  storage:
    retentionPeriod: "1y"
    replicaCount: 2
  requestsLoadBalancer:
    enabled: true
    spec:
     replicaCount: 2
```

 Operator will deploy `VMAuth` deployment with 2 replicas. And update insert and select services to point to `vmauth`.
 In addition, operator will create 3 additional services with the following pattern:

- vtinsertint-CLUSTER_NAME - needed for select pod discovery
- vtselectint-CLUSTER_NAME - needed for insert pod discovery
- vtclusterlb-CLUSTER_NAME - needed for metrics collection and exposing `select` and `insert` components via `VMAuth` balancer.

The `requestsLoadBalancer` feature works transparently and is managed entirely by the `VTCluster` operator, 
with no direct access to the underlying [VMAuth](https://docs.victoriametrics.com/victoriametrics/vmauth/) configuration. 
If you need more control over load balancing behavior, 
or want to combine request routing with authentication or (m)TLS, 
consider deploying a standalone [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth/) resource instead of enabling `requestsLoadBalancer`.


## Examples

```yaml

apiVersion: operator.victoriametrics.com/v1
kind: VTCluster
metadata:
  name: sample
spec:
  insert:
    replicaCount: 1
  select:
    replicaCount: 1
  storage:
    retentionPeriod: "1y"
    replicaCount: 2
```
