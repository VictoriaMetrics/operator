---
sort: 9
weight: 9
title: FAQ
---

# FAQ

## How do you monitor the operator itself?

You can read about vmoperator monitoring in [this document](https://docs.victoriametrics.com/vmoperator/monitoring.html).

## How to change VMStorage PVC storage class

With Helm chart deployment:

1. Update the PVCs manually
2. Run `kubectl delete statefulset --cascade=orphan {vmstorage-sts}` which will delete the sts but keep the pods
3. Update helm chart with the new storage class in the volumeClaimTemplate
4. Run the helm chart to recreate the sts with the updated value

With Operator deployment:

1. Update the PVCs manually
2. Run `kubectl delete vmcluster --cascade=orphan {cluster-name}`
3. Run `kubectl delete statefulset --cascade=orphan {vmstorage-sts}`
4. Update VMCluster spec to use new storage class
5. Apply cluster configuration

## How to override image registry

You can use `VM_CONTAINERREGISTRY` parameter for operator:

- See details about tuning [operator settings here](https://docs.victoriametrics.com/vmoperator/setup.html#settings).
- See [available operator settings](https://docs.victoriametrics.com/vmoperator/vars.html) here.

## How to override image

TODO

## How to set up automatic backups?

You can read about backups setup in [this guide](https://docs.victoriametrics.com/vmoperator/guides/backups.html).

## How to migrate from Prometheus-operator to VictoriaMetrics operator?

You can read about migration from prometheus operator on [this page](https://docs.victoriametrics.com/vmoperator/migration.html).

## How to turn off conversion for prometheus resources

You can read about it on [this page](https://docs.victoriametrics.com/vmoperator/migration.html#objects-convesion).

## My VM objects are not deleted/changed when I delete/change Prometheus objects

You can read about it in following sections of "Migration from prometheus-operator" docs:

- [Deletion synchronization](https://docs.victoriametrics.com/vmoperator/migration.html#deletion-synchronization)
- [Update synchronization](https://docs.victoriametrics.com/vmoperator/migration.html#update-synchronization)
- [Labels synchronization](https://docs.victoriametrics.com/vmoperator/migration.html#labels-synchronization)

## What permissions does an operator need to run in a cluster?

You can read about needed permissions for operator in [this document](https://docs.victoriametrics.com/vmoperator/security.html#roles).

## How to run VictoriaMetrics operator with permissions for one namespace only?

**TODO**

## What versions of Kubernetes is the operator compatible with?

Operator tested at kubernetes versions from 1.16 to 1.22.

**TODO**

For clusters version below 1.16 you must use legacy CRDs from [path](https://github.com/VictoriaMetrics/operator/tree/master/config/crd/legacy)
and disable CRD controller with flag: `--controller.disableCRDOwnership=true`

## What versions of VictoriaMetrics is the operator compatible with?

**TODO**

## How can I set up scrape objects selection?

**TODO**

## **TODO** ArgoCD

**TODO**

## TODO: How to scale/replicate vmoperator?

**TODO**
