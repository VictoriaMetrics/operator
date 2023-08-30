# VMAgent

The `VMAgent` CRD declaratively defines a desired [VMAgent](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmagent)
setup to run in a Kubernetes cluster.

For each `VMAgent` resource Operator deploys a properly configured `Deployment` in the same namespace.
The VMAgent `Pod`s are configured to mount a `Secret` prefixed with `<VMAgent-name>` containing the configuration
for VMAgent.

For each `VMAgent` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with
name `<VMAgent-name>`.

The CRD specifies which `VMServiceScrape` should be covered by the deployed VMAgent instances based on label selection.
The Operator then generates a configuration based on the included `VMServiceScrape`s and updates the `Secret` which
contains the configuration. It continuously does so for all changes that are made to the `VMServiceScrape`s or the
`VMAgent` resource itself.

If no selection of `VMServiceScrape`s is provided - Operator leaves management of the `Secret` to the user,
so user can set custom configuration while still benefiting from the Operator's capabilities of managing VMAgent setups.

## Specification

You can see the full actual specification of the `VMAgent` resource in the [API docs -> VMAgent](https://docs.victoriametrics.com/operator/api.html#vmagent).

## High availability

To run VMAgent in a highly available manner at first you have to configure deduplication in Victoria Metrics
according [this doc for VMSingle](https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html#deduplication)
or [this doc for VMCluster](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#deduplication).

You can do it with `extraArgs` on [`VMSingle`](https://docs.victoriametrics.com/operator/resources/vmsingle.html):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-example
spec:
  # ...
  extraArgs:
    dedup.minScrapeInterval: 5s
```

For [`VMCluster`](https://docs.victoriametrics.com/operator/resources/vmcluster.html) you can do it with `vmstorage.extraArgs` and `vmselect.extraArgs`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: vmcluster-example
spec:
  # ...
  vmselect:
    extraArgs:
      dedup.minScrapeInterval: 5s
    # ...
  vmstorage:
    extraArgs:
      dedup.minScrapeInterval: 5s
    # ...
```

Deduplication is automatically enabled with `replicationFactor>1` on `VMCLuster`.

**TODO**
