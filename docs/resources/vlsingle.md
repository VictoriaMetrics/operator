---
weight: 20
title: VLSingle
menu:
  docs:
    identifier: operator-cr-vlsingle
    parent: operator-cr
    weight: 20
aliases:
  - /operator/resources/vlsingle/
  - /operator/resources/vlsingle/index.html
  - /operator/resources/vlogs/
  - /operator/resources/vlogs/index.html
---
`VLSingle` represents database for storing logs.
The `VLSingle` CRD declaratively defines a [single-node VictoriaLogs](https://docs.victoriametrics.com/victorialogs/)
installation to run in a Kubernetes cluster.

For each `VLSingle` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VLSingle `Pod`s are configured to mount an empty dir or `PersistentVolumeClaimSpec` for storing data.
Deployment update strategy set to [recreate](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#recreate-deployment).
No more than one replica allowed.

For each `VLSingle` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VLogs.metadata.name`.

## Specification

You can see the full actual specification of the `VLSingle` resource in the **[API docs -> VLSingle](https://docs.victoriametrics.com/operator/api#vlsingle)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](./#extra-arguments).

Also, you can check out the [examples](#examples) section.

## High availability

`VLSingle` doesn't support high availability. Consider using [`VLCluster`](https://docs.victoriametrics.com/operator/resources/vlcluster/) or multiple `VLSingle` resources.

## Version management

To set `VLSingle` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLSingle
metadata:
  name: example-vlogs
spec:
  image:
    repository: victoriametrics/victoria-logs
    tag: v1.4.0
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLSingle
metadata:
  name: example-vlogs
spec:
  image:
    repository: victoriametrics/victoria-logs
    tag: v1.4.0
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Resource management

You can specify resources for each `VLSingle` resource in the `spec` section of the `VLogs` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLSingle
metadata:
  name: resources-example
spec:
    # ...
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
by default all `VLSingle` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration):

- `VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VLSingle` pods,
- `VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VLSingle` pods,
- `VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VLSingle` pods,
- `VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VLSingle` pods.

These default parameters will be used if:

- `VM_VLOGSDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VLSingle` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VLSingle` spec have higher priority than operator parameters.

If you set `VM_VLOGSDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VLSingle` CRD,
then `VLSingle` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Examples

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLSingle
metadata:
  name: example
spec:
  retentionPeriod: "12"
  storage:
    resources:
      requests:
        storage: 50Gi
  resources:
    requests:
      memory: 500Mi
      cpu: 500m
    limits:
      memory: 10Gi
      cpu: 5
```

## Migration from VLogs

 At first, create a new `VLSingle` resource, point clients to the new resource. Use [backup-restore](https://docs.victoriametrics.com/victorialogs/#backup-and-restore) procedure for historical data migration.
