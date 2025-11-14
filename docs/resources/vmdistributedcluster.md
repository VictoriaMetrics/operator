---
weight: 12
title: VMDistributedCluster
menu:
  docs:
    identifier: operator-cr-vmdistributedcluster
    parent: operator-cr
    weight: 12
aliases:
  - /operator/resources/vmdistributedcluster/
tags:
  - CRD
  - VMDistributedCluster
---

`VMDistributedCluster` is the Custom Resource Definition for orchestrated update of several VictoriaMetrics clusters. It allows you to define and manage cluster components of a distributed VictoriaMetrics setup and apply changes to them without downtime or disruption.

For a high-level overview of VictoriaMetrics distributed cluster architecture, refer to the official [VictoriaMetrics documentation](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/).

## Specification

The `VMDistributedCluster` resource allows you to configure various aspects of your VictoriaMetrics distributed cluster. The specification includes details about:

*   `paused`: If set to `true`, the operator will not perform any actions on the underlying managed objects. Useful for temporarily halting reconciliation.
*   `zones`: A list of objects, where each entry either defines or references a `VMCluster` instance.
*   `vmagent`: A reference to a `VMAgent` object that will be configured to scrape metrics from all `VMCluster` instances managed by this `VMDistributedCluster`.
*   `vmusers`: A list of references to `VMUser` objects. These `VMUser`s can be used to control access and routing to the managed `VMCluster` instances.

### `VMClusterRefOrSpec`

Each entry in the `zones` array is a `VMClusterRefOrSpec` object. This flexible structure allows you to either reference an existing `VMCluster` resource or define a new one inline.

A `VMClusterRefOrSpec` object has the following mutually exclusive fields:

*   `ref`: A `corev1.LocalObjectReference` pointing to an existing `VMCluster` object by name. If `ref` is specified, `name` and `spec` are ignored.
*   `overrideSpec`: An optional JSON object that specifies an override to the `VMClusterSpec` of the referenced object when `ref` is used. This allows applying common configuration changes across referenced clusters without modifying their original definitions.
*   `name`: A static name to be used for the `VMCluster` when `spec` is provided. This field is ignored if `ref` is specified.
*   `spec`: A `vmv1beta1.VMClusterSpec` object that defines the desired state of a new `VMCluster`. This field is ignored if `ref` is specified.

**Example: Defining a `VMDistributedCluster` with inline `VMCluster` specifications for two zones:**

```yaml
apiVersion: operator.victoriametrics.com/v1alpha1
kind: VMDistributedCluster
metadata:
  name: my-distributed-cluster
spec:
  vmagent:
    name: my-distributed-vmagent
  vmusers:
    - name: my-read-user-for-zone-a
    - name: my-read-user-for-zone-b
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
  paused: false
```

**Example: Referencing existing `VMCluster` objects and applying an `overrideSpec`:**

```yaml
apiVersion: operator.victoriametrics.com/v1alpha1
kind: VMDistributedCluster
metadata:
  name: referenced-distributed-cluster
spec:
  zones:
    - vmclusters:
        ref:
          name: existing-cluster-zone-1
        overrideSpec:
          vmstorage: # Increase PVC for vmstorage in this zone
            storage:
              volumeClaimTemplate:
                spec:
                  resources:
                    requests:
                      storage: 20Gi
    - vmclusters:
        ref:
          name: existing-cluster-zone-2
        overrideSpec:
          vmstorage: # Increase vmstorage replicas for this zone
            replicaCount: 2
```

### Current shortcomings
- When VMDistributedCluster is removed, all resources created inline are not deleted.
- Only VMCluster objects are supported
- Only one VMAgent can be referenced
- All objects must belong to the same namespace as VMDistributedCluster
- Referenced VMClusters are not being actively watched for changes, they only get reconciled periodically
- All objects must be referred to by name, label selectors are not supported
