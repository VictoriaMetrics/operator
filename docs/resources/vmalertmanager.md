# VMAlertmanager

The `VMAlertmanager` CRD declaratively defines a desired Alertmanager setup to run in a Kubernetes cluster.
It provides options to configure replication and persistent storage.

For each `Alertmanager` resource, the Operator deploys a properly configured `StatefulSet` in the same namespace.
The Alertmanager pods are configured to include a `Secret` called `<alertmanager-name>` which holds the used
configuration file in the key `alertmanager.yaml`.

When there are two or more configured replicas the Operator runs the Alertmanager instances in high availability mode.

## Specification

You can see the full actual specification of the `VMAlertmanager` resource in the [API docs -> VMAlert](https://docs.victoriametrics.com/operator/api.html#vmalertmanager).

## High Availability

The final step of the high availability scheme is Alertmanager, when an alert triggers, actually fire alerts against *all* instances of an Alertmanager cluster.

The Alertmanager, starting with the `v0.5.0` release, ships with a high availability mode. 
It implements a gossip protocol to synchronize instances of an Alertmanager cluster 
regarding notifications that have been sent out, to prevent duplicate notifications. 
It is an AP (available and partition tolerant) system. Being an AP system means that notifications are guaranteed to be sent at least once.

The Victoria Metrics Operator ensures that Alertmanager clusters are properly configured to run highly available on Kubernetes.
