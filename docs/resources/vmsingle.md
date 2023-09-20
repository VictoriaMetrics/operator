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
