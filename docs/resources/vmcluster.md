# VMCluster

The `VMCluster` CRD defines a [cluster version VM](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html).

For each `VMCluster` resource, the Operator creates:

- `VMStorage` as `StatefulSet`,
- `VMSelect` as `StatefulSet`
- and `VMInsert` as deployment.

For `VMStorage` and `VMSelect` headless  services are created. `VMInsert` is created as service with clusterIP.

There is a strict order for these objects creation and reconciliation:

1. `VMStorage` is synced - the Operator waits until all its pods are ready;
2. Then it syncs `VMSelect` with the same manner;
3. `VMInsert` is the last object to sync.

All [statefulsets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) are created 
with [OnDelete](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#on-delete) update type. 
It allows to manually manage the rolling update process for Operator by deleting pods one by one and waiting for the ready status.

Rolling update process may be configured by the operator env variables.
The most important is `VM_PODWAITREADYTIMEOUT=80s` - it controls how long to wait for pod's ready status.

## Specification

You can see the full actual specification of the `VMCluster` resource in the [API docs -> VMCluster](https://docs.victoriametrics.com/vmoperator/api.html#vmcluster).

## High availability

The cluster version provides a full set of high availability features - metrics replication, node failover, horizontal scaling.

First, we recommend familiarizing yourself with the high availability tools provided by "VictoriaMetrics Cluster" itself:

- [High availability](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#high-availability),
- [Cluster availability](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#cluster-availability),
- [Replication and data safety](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#replication-and-data-safety).

`VMCluster` supports all listed in the above-mentioned articles parameters and features:

- `replicationFactor` - the number of replicas for each metric.
- for every component of cluster (`vmstorage` / `vmselect` / `vminsert`):
  - `replicaCount` - the number of replicas for components of cluster.
  - `affinity` - the affinity (the pod's scheduling constraints) for components pods. See more details in [kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity).
  - `topologySpreadConstraints` - controls how pods are spread across your cluster among failure-domains such as regions, zones, nodes, and other user-defined topology domains. See more details in [kubernetes docs](https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/).

In addition, operator:

- uses k8s services or vmauth for load balancing between `vminsert` and `vmselect` components,
- uses health checks for to determine the readiness of components for work after restart,
- allows to horizontally scale all cluster components just by changing `replicaCount` field.

Here is an example of a `VMCluster` resource with HA features:

```yaml
# example-vmcluster-persistent.yaml

apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-vmcluster-persistent
spec:
  replicationFactor: 2       
  vmstorage:
    replicaCount: 10
    storageDataPath: "/vm-data"
    affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: "app.kubernetes.io/name"
                operator: In
                values:
                - "vmstorage"
            topologyKey: "kubernetes.io/hostname"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 10Gi
    resources:
      limits:
        cpu: "2"
        memory: 2048Mi
  vmselect:
    replicaCount: 3
    cacheMountPath: "/select-cache"
    affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: "app.kubernetes.io/name"
                operator: In
                values:
                - "vmselect"
            topologyKey: "kubernetes.io/hostname"

    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 2Gi
    resources:
      limits:
        cpu: "1"
        memory: "500Mi"
  vminsert:
    replicaCount: 4
    affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: "app.kubernetes.io/name"
                operator: In
                values:
                - "vminsert"
            topologyKey: "kubernetes.io/hostname"
    resources:
      limits:
        cpu: "1"
        memory: "500Mi"
```
