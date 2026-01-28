---
weight: 12
title: VMDistributed
menu:
  docs:
    identifier: operator-cr-vmdistributed
    parent: operator-cr
    weight: 12
aliases:
  - /operator/resources/vmdistributed/
tags:
  - vmdistributed
---

`VMDistributed` is the Custom Resource Definition for orchestrated updates of several VictoriaMetrics clusters. It allows you to define and manage cluster components of a distributed VictoriaMetrics setup and apply changes to them sequentially, ensuring high availability and minimal disruption.

For a high-level overview of VictoriaMetrics distributed cluster architecture, refer to the official [VictoriaMetrics documentation](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/).

## Specification

The `VMDistributed` resource allows you to configure various aspects of your VictoriaMetrics distributed cluster. The specification includes:

*   `retain`: If set to `true`, the operator will keep managed components during VMDistributed CR removal.
*   `paused`: If set to `true`, the operator will not perform any actions on the underlying managed objects. Useful for temporarily halting reconciliation.
*   `vmagent`: Configuration for a `VMAgent` instance that will be configured to route traffic to managed `VMCluster` instances. It acts as a global write path entry point.
*   `vmauth`: Configuration for a `VMAuth` instance that acts as a proxy/load-balancer for the `vmselect` components of the managed clusters. It acts as a global read path entry point.
*   `zoneCommon`: Defines `VMDistributedZoneCommon` common zone configuration, which is default for each zone resource.
*   `zones`: Defines the list of `VMDistributedZone` that form the distributed setup.
*   `license`: Configures the license key for enterprise features. If provided, it is automatically passed to the managed `VMAgent`, `VMAuth`, and all `VMCluster` instances.

### `VMDistributedZoneCommon`

*   `readyTimeout`: The readiness timeout for each zone update. Default is `5m`.
*   `updatePause`: Time the operator should wait between zone updates to ensure a smooth transition. Default is `1m`.

### `VMDistributedZone`

Each entry in the `zones` array includes:

*   `name` defines name of zone, which is used as for default per zone resource name or can be used in `zoneCommon` template using `%ZONE%` placeholder.
*   `vmcluster` defines `VMDistributedZoneCluster`, that allows either reference an existing `VMCluster` resource or define a new one inline.

### `VMDistributedZoneCluster`

*   `name`: The name of existing `VMCluster` or one, that is expected to be created.
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
    - name: zone-a
      vmcluster:
        name: zone-a    # if omitted it's expected to be `${metadata.name}-${zone.name}`
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
      vmcluster:
        name: zone-b
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
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: cluster-prod-1
spec:
  vmstorage:
    replicaCount: 1
  vmselect:
    replicaCount: 1
  vminsert:
    replicaCount: 1
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: cluster-prod-2
spec:
  vmstorage:
    replicaCount: 1
  vmselect:
    replicaCount: 1
  vminsert:
    replicaCount: 1
---
apiVersion: operator.victoriametrics.com/v1alpha1
kind: VMDistributed
metadata:
  name: managed-distributed-cluster
spec:
  zoneCommon:
    vmstorage:
      spec:
        replicaCount: 3
  zones:
    - name: zone-1
      vmcluster:
        name: cluster-prod-1
        spec:
          vmstorage:
            storage:
              volumeClaimTemplate:
                spec:
                  resources:
                    requests:
                      storage: 50Gi
    - name: zone-2
      vmcluster:
        name: cluster-prod-2
```

### VMDistributed and distributed chart

VMDistributed can be used alongside the resources created by the distributed chart. The distributed chart provides a convenient way to create multiple `VMCluster` objects and surrounding resources.

In order to update VMClusters in a coordinated manner, add VMCluster resources to the `zones` list:
```
spec:
  zones:
    - name: zone-1 
      vmcluster:
        name: cluster-1
    - name: zone-2
      vmcluster:
        name: cluster-2
    - name: zone-3
      vmcluster:
        name: cluster-3
```

and set vmauth pointing to global read vmauth:
```
spec:
  vmauth:
    name: vmauth-global-read-<release name>
```

### Ownership and references

VMDistributed owns VMAgents, and VMAuths created or referenced by the distributed chart with the same namespace as the VMDistributed. Only created ones are deleted when the VMDistributed is deleted.

When VMCluster is referenced via `ref`, these objects will have `ownerRef` set to the VMDistributed, but they will not be deleted when the VMDistributed is deleted. Instead, only `ownerRef` will be removed from them.

### Current shortcomings
- Only `VMCluster` objects are supported for distributed management.
- Only one `VMAgent` and one `VMAuth` can be managed per `VMDistributed`.
- All objects must belong to the same namespace as the `VMDistributed`.
- Referenced `VMCluster` objects (using `ref`) are not actively watched for external changes; they are reconciled periodically or when the `VMDistributed` itself changes.
- Objects must be referred to by name; label selectors are not supported for cluster selection.
