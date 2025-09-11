---
weight: 20
title: VTSingle
menu:
  docs:
    identifier: operator-cr-vtsingle
    parent: operator-cr
    weight: 20
aliases:
  - /operator/resources/vtsingle/
  - /operator/resources/vtsingle/index.html
tags:
  - kubernetes
  - metrics
---
`VTSingle` represents database for storing traces.
The `VTSingle` CRD declaratively defines a [single-node VictoriaTraces](https://docs.victoriametrics.com/victoriatraces/)
installation to run in a Kubernetes cluster.

For each `VTSingle` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VTSingle `Pod`s are configured to mount an empty dir or `PersistentVolumeClaimSpec` for storing data.
Deployment update strategy set to [recreate](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#recreate-deployment).
No more than one replica allowed.

For each `VTSingle` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VTSingle.metadata.name`.

## Specification

You can see the full actual specification of the `VTSingle` resource in the **[API docs -> VTSingle](https://docs.victoriametrics.com/operator/api/#vtsingle)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vtsingle/#examples) section.

## High availability

`VTSingle` doesn't support high availability. Consider using [`VTCluster`](https://docs.victoriametrics.com/operator/resources/vtcluster/) or multiple `VTSingle` resources.

## Version management

To set `VTSingle` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTSingle
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/victoria-traces
    tag: v0.1.0
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTSingle
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/victoria-traces
    tag: v1.4.0
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Resource management

You can specify resources for each `VTSingle` resource in the `spec` section of the `VTSingle` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTSingle
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
by default all `VTSingle` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VTSINGLEDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VTSingle` pods,
- `VM_VTSINGLEDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VTSingle` pods,
- `VM_VTSINGLEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VTSingle` pods,
- `VM_VTSINGLEDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VTSingle` pods.

These default parameters will be used if:

- `VM_VTSINGLEDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VTSingle` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VTSingle` spec have higher priority than operator parameters.

If you set `VM_VTSINGLEDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VTSingle` CRD,
then `VTSingle` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Examples


### VTSingle with resources set

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTSingle
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

### VTSingle with existing volume

create PVC and bind it to existing PV `existing-pv-name`

> `spec.storageClassName` should match a storage class name of PersistentVolume

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: standard
  volumeName: existing-pv-name
```

create VTSingle with PVC `example-pvc` mounted

> `spec.replicaCount` should be set to `1` since existing volume can only be mounted once

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTSingle
metadata:
  name: example
spec:
  replicaCount: 1
  retentionPeriod: "12"
  extraArgs:
    storageDataPath: /vt-data
  volumes:
    - name: vtstorage
      persistentVolumeClaim:
        claimName: example-pvc
  volumeMounts:
    - name: vtstorage
      mountPath: /vt-data
  resources:
    requests:
      memory: 500Mi
      cpu: 500m
    limits:
      memory: 10Gi
      cpu: 5
```
