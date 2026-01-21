---
weight: 12
title: VMDistributed
menu:
  docs:
    identifier: operator-cr-VMDistributed
    parent: operator-cr
    weight: 12
aliases:
  - /operator/resources/VMDistributed/
tags:
  - CRD
  - VMDistributed
---

`VMDistributed` is the Custom Resource Definition for orchestrated updates of several VictoriaMetrics clusters. It allows you to define and manage cluster components of a distributed VictoriaMetrics setup and apply changes to them sequentially, ensuring high availability and minimal disruption.

For a high-level overview of VictoriaMetrics distributed cluster architecture, refer to the official [VictoriaMetrics documentation](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/).

## Specification

The `VMDistributed` resource allows you to configure various aspects of your VictoriaMetrics distributed cluster. The specification includes:

*   `paused`: If set to `true`, the operator will not perform any actions on the underlying managed objects. Useful for temporarily halting reconciliation.
*   `readyDeadline`: The deadline for each `VMCluster` to become ready during an update. Default is `5m`.
*   `vmAgentFlushDeadline`: The deadline for `VMAgent` to flush its accumulated queue before proceeding to the next cluster update. Default is `1m`.
*   `zoneUpdatePause`: The time the operator should wait between zone updates to ensure a smooth transition. Default is `1m`.
*   `vmagent`: Configuration for a `VMAgent` instance that will be configured to route traffic to managed `VMCluster` instances. It acts as a global write path entry point.
*   `vmauth`: Configuration for a `VMAuth` instance that acts as a proxy/load-balancer for the `vmselect` components of the managed clusters. It acts as a global read path entry point.
*   `zones`: Defines the set of VictoriaMetrics clusters that form the distributed setup.
    *   `globalOverrideSpec`: An optional JSON object that specifies an override to the `VMClusterSpec` applied to **all** clusters in the `zones` list.
    *   `vmclusters`: A list of `VMClusterRefOrSpec` objects defining the individual clusters.
*   `license`: Configures the license key for enterprise features. If provided, it is automatically passed to the managed `VMAgent`, `VMAuth`, and all `VMCluster` instances.

### `VMClusterRefOrSpec`

Each entry in the `vmclusters` array allows you to either reference an existing `VMCluster` resource or define a new one inline.

*   `ref`: A `corev1.LocalObjectReference` pointing to an existing `VMCluster` object by name.
*   `name`: The name to be used for the `VMCluster` when `spec` is provided.
*   `spec`: A `vmv1beta1.VMClusterSpec` object that defines the desired state of a new or referenced `VMCluster` managed by this resource.

**Example: Defining a `VMDistributed` with inline `VMCluster` specifications:**

```yaml
apiVersion: operator.victoriametrics.com/v1alpha1
kind: VMDistributed
metadata:
  name: my-distributed-cluster
spec:
  vmagent:
    name: my-distributed-vmagent
  vmauth:
    name: my-distributed-vmauth
  zones:
    vmclusters:
      - name: zone-a
        spec:
          vmstorage:
            replicaCount: 1
            storage:
              volumeClaimTemplate:
                spec:
                  resources:
                    requests:
                      storage: 10Gi
          vmselect:
            replicaCount: 1
          vminsert:
            replicaCount: 1
      - name: zone-b
        spec:
          vmstorage:
            replicaCount: 1
            storage:
              volumeClaimTemplate:
                spec:
                  resources:
                    requests:
                      storage: 10Gi
          vmselect:
            replicaCount: 1
          vminsert:
            replicaCount: 1
```

**Example: Referencing existing clusters with global and specific overrides:**

```yaml
apiVersion: operator.victoriametrics.com/v1alpha1
kind: VMDistributed
metadata:
  name: managed-distributed-cluster
spec:
  zones:
    globalOverrideSpec:
      vmstorage:
        replicaCount: 3
    vmclusters:
      - ref:
          name: cluster-prod-1
        spec:
          vmstorage:
            storage:
              volumeClaimTemplate:
                spec:
                  resources:
                    requests:
                      storage: 50Gi
      - ref:
          name: cluster-prod-2
```

### VMDistributed and distributed chart

VMDistributed can be used alongside the resources created by the distributed chart. The distributed chart provides a convenient way to create multiple `VMCluster` objects and surrounding resources.

In order to update VMClusters in a coordinated manner, add VMCluster resources to the `vmclusters` list as refs:
```
    vmclusters:
      - ref:
          name: cluster-1
      - ref:
          name: cluster-2
      - ref:
          name: cluster-3
```

and set vmauth pointing to global read vmauth:
```
  vmauth:
    name: vmauth-global-read-<release name>
```

VMAgents can be referenced by a label selector:
```
  vmagent:
    labelSelector:
      matchLabels:
        app: vmagent
```

### Ownership and references

VMDistributed owns VMAgents, and VMAuths created or referenced by the distributed chart. These objects are created with the same namespace as the VMDistributed and will be deleted when the VMDistributed is deleted.

When VMCluster is referenced via `ref`, these objects will have `ownerRef` set to the VMDistributed, but they will not be deleted when the VMDistributed is deleted. Instead, only `ownerRef` will be removed from them.

### Current shortcomings
- Only `VMCluster` objects are supported for distributed management.
- Only one `VMAgent` and one `VMAuth` can be managed per `VMDistributed`.
- All objects must belong to the same namespace as the `VMDistributed`.
- Referenced `VMCluster` objects (using `ref`) are not actively watched for external changes; they are reconciled periodically or when the `VMDistributed` itself changes.
- Objects must be referred to by name; label selectors are not supported for cluster selection.
