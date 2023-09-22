# VMSingle

`VMSingle` represents database for storing metrics.
The `VMSingle` CRD declaratively defines a [single-node VM](https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html)
installation to run in a Kubernetes cluster.

For each `VMSingle` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VMSingle `Pod`s are configured to mount an empty dir or `PersistentVolumeClaimSpec` for storing data.
Deployment update strategy set to [recreate](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#recreate-deployment).
No more than one replica allowed.

For each `VMSingle` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VMSingle.metadata.name`.

## Specification

You can see the full actual specification of the `VMSingle` resource in the **[API docs -> VMSingle](https://docs.victoriametrics.com/operator/api.html#vmsingle)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-args).

Also, you can check out the [examples](#examples) section.

## High availability

`VMSingle` doesn't support high availability by default, for such purpose
use [`VMCluster`](https://docs.victoriametrics.com/operator/resources/vmcluster.html) instead or duplicate the setup.

## Version management

To set `VMSingle` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example-vmsingle
spec:
  image:
    repository: victoriametrics/victoria-metrics
    tag: v1.93.4
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example-vmsingle
spec:
  image:
    repository: victoriametrics/victoria-metrics
    tag: v1.93.4
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Enterprise features

VMSingle supports features [Downsampling](https://docs.victoriametrics.com/#downsampling) 
and [Multiple retentions / Retention filters](https://docs.victoriametrics.com/#retention-filters)
from [VictoriaMetrics Enterprise](https://docs.victoriametrics.com/enterprise.html#victoriametrics-enterprise).

For using Enterprise version of [vmsingle](https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html)
you need to change version of `VMSingle` to version with `-enterprise` suffix using [Version management](#version-management).

All the enterprise apps require `-eula` command-line flag to be passed to them.
This flag acknowledges that your usage fits one of the cases listed on [this page](https://docs.victoriametrics.com/enterprise.html#victoriametrics-enterprise).
So you can use [extraArgs](https://docs.victoriametrics.com/operator/resources/#extra-args) for passing this flag to `VMSingle`:

### Downsampling

After that you can pass [Downsampling](https://docs.victoriametrics.com/#downsampling)
flag to `VMSingle` with [extraArgs](https://docs.victoriametrics.com/operator/resources/#extra-args) too.

Here are complete example for [Downsampling](https://docs.victoriametrics.com/#downsampling):
 
```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-ent-example
spec:
  # enabling enterprise features
  image:
    # enterprise version of vmsingle
    tag: v1.93.5-enterprise
  extraArgs:
    # should be true and means that you have the legal right to run a vmsingle enterprise
    # that can either be a signed contract or an email with confirmation to run the service in a trial period
    # https://victoriametrics.com/legal/esa/
    eula: true
    
    # using enterprise features: Downsampling
    # more details about downsampling you can read on https://docs.victoriametrics.com/#downsampling
    downsampling.period: 30d:5m,180d:1h,1y:6h,2y:1d

  # ...other fields...
```

### Retention filters

The same method is used to enable retention filters - here are complete example for [Retention filters](https://docs.victoriametrics.com/#retention-filters).

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-ent-example
spec:
  # enabling enterprise features
  image:
    # enterprise version of vmsingle
    tag: v1.93.5-enterprise
  extraArgs:
    # should be true and means that you have the legal right to run a vmsingle enterprise
    # that can either be a signed contract or an email with confirmation to run the service in a trial period
    # https://victoriametrics.com/legal/esa/
    eula: true
    
    # using enterprise features: Retention filters
    # more details about retention filters you can read on https://docs.victoriametrics.com/#retention-filters
    retentionFilter: '{team="juniors"}:3d,{env=~"dev|staging"}:30d'

  # ...other fields...
```

## Examples

```yaml
kind: VMSingle
metadata:
  name: vmsingle-example
spec:
  retentionPeriod: "12"
  removePvcAfterDelete: true
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 50Gi
  extraArgs:
    dedup.minScrapeInterval: 60s
  resources:
    requests:
      memory: 500Mi
      cpu: 500m
    limits:
      memory: 10Gi
      cpu: 5
```
