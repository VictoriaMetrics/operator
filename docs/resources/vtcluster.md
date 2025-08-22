---
weight: 22
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
see [Extra arguments section](./#extra-arguments).

Also, you can check out the [examples](#examples) section.

## Version management

To set `VTCluster` version add `spec.clusterVersion` or `spec.COMPONENT.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

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
by default all `VTCluster` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration):

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

 Operator provides enhanced load-balancing mechanism for `insert` and `select` clients. By default, operator uses built-in Kubernetes [service]() with `clusterIP` type for clients connection. It's good solution for short lived connections. But it acts poorly with long-lived TCP sessions and leads to the uneven resources utilization for `select` and `insert` components.

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
