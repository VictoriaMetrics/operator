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

**Note:** `VMDistributed` is an experimental feature and may not be suitable for production environments. API is not yet stabilized and may change in future releases.

For a high-level overview of VictoriaMetrics distributed cluster architecture, refer to the official [VictoriaMetrics documentation](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/).

## Specification

The `VMDistributed` resource allows you to configure various aspects of your VictoriaMetrics distributed cluster. The specification includes:

*   `retain`: If set to `true`, the operator will keep managed components during VMDistributed CR removal.
*   `paused`: If set to `true`, the operator will not perform any actions on the underlying managed objects. Useful for temporarily halting reconciliation.
*   `vmauth`: Configuration for a `VMAuth` instance that acts as a proxy/load-balancer for the `vmselect` and `vmagent` components of the managed clusters. It acts as a global read and write path entry point.
*   `zoneCommon`: Defines `VMDistributedZoneCommon` common zone configuration, which is default for each zone resource.
*   `zones`: Defines the list of `VMDistributedZone` that form the distributed setup.
*   `license`: Configures the license key for enterprise features. If provided, it is automatically passed to the managed `VMAuth`, all `VMAgent`, and `VMCluster` instances.

### `VMDistributedZoneCommon`

*   `readyTimeout`: The readiness timeout for each zone update. Default is `5m`.
*   `updatePause`: Time the operator should wait between zone updates to ensure a smooth transition. Default is `1m`.

### `VMDistributedZone`

Each entry in the `zones` array includes:

*   `name` defines name of zone, which is used as for default per zone resource name or can be used in `zoneCommon` template using `%ZONE%` placeholder.
*   `vmcluster` defines `VMDistributedZoneCluster`, that allows either reference an existing `VMCluster` resource or creates a new one.
*   `vmagent` defines `VMDistributedZoneAgent`, that allows either reference an existing `VMAgent` resource or creates a one. It receives write traffic from `vmauth` and sends to `vmcluster` instances.

### `VMDistributedZoneCluster`

*   `name`: Optional name of an existing `VMCluster` or one to be created; when omitted it defaults to  `zoneCommon.vmcluster.name` (where `%ZONE%` is used to template the zone name) or `${cr.Name}-${zone.name}` when unset.
*   `spec`: A `vmv1beta1.VMClusterSpec` object that defines the desired state of a new or referenced `VMCluster` managed by this resource.

**Example: Defining a `VMDistributed` resource:**

```yaml
apiVersion: operator.victoriametrics.com/v1alpha1
kind: VMDistributed
metadata:
  name: my-distributed-cluster
spec:
  vmauth:
    name: my-distributed-vmauth
    spec:
      replicaCount: 1
  zoneCommon:
    vmagent:
      spec:
        replicaCount: 1
    vmcluster:
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
  zones:
    - name: zone-a
    - name: zone-b
```

### VMDistributed and distributed chart

VMDistributed can be used alongside the resources created by the distributed chart. The distributed chart provides a convenient way to create multiple `VMCluster` objects and surrounding resources.

In order to update VMClusters in a coordinated manner, add VMCluster resources to the `zones` list:
```
spec:
  vmauth:
    spec:
      replicaCount: 1
  zoneCommon:
    vmagent:
      name: vmagent-%ZONE%
      spec:
        replicaCount: 1
    vmcluster:
      name: vmcluster-%ZONE%
      spec:
        replicationFactor: 1
        requestsLoadBalancer:
          enabled: true
          spec:
            nodeSelector:
              kubernetes.io/os: linux
            replicaCount: 2
            topologySpreadConstraints:
            - maxSkew: 1
              topologyKey: kubernetes.io/hostname
              whenUnsatisfiable: ScheduleAnyway
        retentionPeriod: "14"
        vminsert:
          nodeSelector:
            kubernetes.io/os: linux
          port: "8480"
          replicaCount: 2
          topologySpreadConstraints:
          - maxSkew: 1
            topologyKey: kubernetes.io/hostname
            whenUnsatisfiable: ScheduleAnyway
        vmselect:
          nodeSelector:
            kubernetes.io/os: linux
          port: "8481"
          replicaCount: 2
          topologySpreadConstraints:
          - maxSkew: 1
            topologyKey: kubernetes.io/hostname
            whenUnsatisfiable: ScheduleAnyway
        vmstorage:
          nodeSelector:
            kubernetes.io/os: linux
          replicaCount: 2
          storageDataPath: /vm-data
          topologySpreadConstraints:
          - maxSkew: 1
            topologyKey: kubernetes.io/hostname
            whenUnsatisfiable: ScheduleAnyway
  zones:
    - name: central
    - name: east
    - name: west
```

Note that VMDistributed will create a single VMAuth, which encapsulates rules for both read and write access to the zones, unlike the distributed chart which creates separate VMAuths for read and write paths. This provides a convenient way to migrate the traffic between VMDistributed and distributed helm chart implementations.

### Ownership and references

VMDistributed creates or becomes an owner of existing VMAuth, VMAgent and VMCluster resources and by default it removes all resources it owns on CR deletion. To remove VMDistributed but keep all owned resources set `spec.retain: true` before VMDistributed removal.

### Current shortcomings
- Only `VMCluster` objects are supported for distributed management.
- Only one `VMAuth` can be managed per `VMDistributed`.
- All objects must belong to the same namespace as the `VMDistributed`.
- Objects must be referred to by name; label selectors are not supported for cluster selection.
