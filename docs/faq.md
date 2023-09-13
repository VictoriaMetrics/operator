---
sort: 9
weight: 9
title: FAQ
---

# FAQ

## How do you monitor the operator itself?

You can read about vmoperator monitoring in [this document](https://docs.victoriametrics.com/operator/monitoring.html).

## How to change VMStorage PVC storage class

With Helm chart deployment:

1. Update the PVCs manually
1. Run `kubectl delete statefulset --cascade=orphan {vmstorage-sts}` which will delete the sts but keep the pods
1. Update helm chart with the new storage class in the volumeClaimTemplate
1. Run the helm chart to recreate the sts with the updated value

With Operator deployment:

1. Update the PVCs manually
1. Run `kubectl delete vmcluster --cascade=orphan {cluster-name}`
1. Run `kubectl delete statefulset --cascade=orphan {vmstorage-sts}`
1. Update VMCluster spec to use new storage class
1. Apply cluster configuration

## How to override image registry

You can use `VM_CONTAINERREGISTRY` parameter for operator:

- See details about tuning [operator settings here](https://docs.victoriametrics.com/operator/setup.html#settings).
- See [available operator settings](https://docs.victoriametrics.com/operator/vars.html) here.

## How to set up automatic backups?

You can read about backups setup in [this guide](https://docs.victoriametrics.com/operator/guides/backups.html).

## How to migrate from Prometheus-operator to VictoriaMetrics operator?

You can read about migration from prometheus operator on [this page](https://docs.victoriametrics.com/operator/migration.html).

## How to turn off conversion for prometheus resources

You can read about it on [this page](https://docs.victoriametrics.com/operator/migration.html#objects-convesion).

## My VM objects are not deleted/changed when I delete/change Prometheus objects

You can read about it in following sections of "Migration from prometheus-operator" docs:

- [Deletion synchronization](https://docs.victoriametrics.com/operator/migration.html#deletion-synchronization)
- [Update synchronization](https://docs.victoriametrics.com/operator/migration.html#update-synchronization)
- [Labels synchronization](https://docs.victoriametrics.com/operator/migration.html#labels-synchronization)

## What permissions does an operator need to run in a cluster?

You can read about needed permissions for operator in [this document](https://docs.victoriametrics.com/operator/security.html#roles).

## How to run VictoriaMetrics operator with permissions for one namespace only?

See this document for details: [Configuration -> Namespaced mode](https://docs.victoriametrics.com/operator/configuration.html#namespaced-mode).

## What versions of Kubernetes is the operator compatible with?

Operator tested at kubernetes versions from 1.16 to 1.23.

For clusters version below 1.16 you must use legacy CRDs from [path](https://github.com/VictoriaMetrics/operator/tree/master/config/crd/legacy)
and disable CRD controller with flag: `--controller.disableCRDOwnership=true`
