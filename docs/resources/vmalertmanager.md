# VMAlertmanager

The `VMAlertmanager` CRD declaratively defines a desired Alertmanager setup to run in a Kubernetes cluster.
It provides options to configure replication and persistent storage.

For each `Alertmanager` resource, the Operator deploys a properly configured `StatefulSet` in the same namespace.
The Alertmanager pods are configured to include a `Secret` called `<alertmanager-name>` which holds the used
configuration file in the key `alertmanager.yaml`.

When there are two or more configured replicas the Operator runs the Alertmanager instances in high availability mode.

## Specification

You can see the full actual specification of the `VMAlertmanager` resource in the [API docs -> VMAlert](https://docs.victoriametrics.com/vmoperator/api.html#vmalertmanager).

## High Availability

**TODO**
