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
`VLCluster` represents database for storing logs.
The `VLCluster` CRD declaratively defines a [VictoriaLogs cluster](https://docs.victoriametrics.com/victorialogs/cluster/)
installation to run in a Kubernetes cluster.

For each `VLCluster` resource, the Operator deploys a properly configured components `VLInsert`'s `Deployment`, `VLSelect`'s `Deployment` and `VLStorage`'s `StatefulSet` in the same namespace.
The VLStorage `Pod`s are configured to mount an empty dir or `StorageSpec` for storing data.

For each `VLCluster` resource components, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VLCluster.metadata.name`.

## Specification

You can see the full actual specification of the `VLCluster` resource in the **[API docs -> VLCluster](https://docs.victoriametrics.com/operator/api#vlcluster)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](./#extra-arguments).

Also, you can check out the [examples](#examples) section.

## Version management

To set `VLCluster` version add `spec.clusterVersion` or `spec.COMPONENT.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLCluster
metadata:
  name: example
spec:
  ...
  clusterVersion: v1.22.0-victoria-logs
  vlstorage:
    image:
      repository: victoriametrics/victoria-logs
      tag: v1.19.0-victoria-logs
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
by default all `VLCluster` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration):

- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_LIMIT_MEM:` - default memory limit for `VLCluster.vlstorage` pods,
- `VLSTORAGEDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VLCluster.vlstorage` pods,
- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VLCluster.vlstorage` pods,
- `VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VLCluster.vlstorage` pods.

These default parameters will be used if:

- `VM_VLCLUSTERDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VLCluster` CR doesn't have `resources` field in `spec` section for component.

Field `resources` in `VLSingle` spec have higher priority than operator parameters.

If you set `VM_VLOGSDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VLSingle` CRD,
then `VLSingle` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Requests Load-Balancing

 Operator provides enhanced load-balancing mechanism for `vlinsert` and `vlselect` clients. By default, operator uses built-in Kubernetes [service]() with `clusterIP` type for clients connection. It's good solution for short lived connections. But it acts poorly with long-lived TCP sessions and leads to the uneven resources utilization for `vlselect` and `vlinsert` components.

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
with no direct access to the underlying [VMAuth](https://docs.victoriametrics.com/vmauth/) configuration. 
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
