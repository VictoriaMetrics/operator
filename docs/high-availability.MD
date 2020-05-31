
# High Availability

High availability is not only important for customer facing software, but if the monitoring infrastructure is not highly available, then there is a risk that operations people are not notified for alerts of the customer facing software. Therefore high availability must be just as thought through for the monitoring stack, as for anything else.

## VmAgent

To run VmAgent in a highly available manner you have to configure deduplication at Victoria Metrics first [doc](https://github.com/VictoriaMetrics/VictoriaMetrics/blob/master/docs/Single-server-VictoriaMetrics.md#deduplication)

Then increase replicas for vmAgent.

## VmAlert

It can be launched with multiple replicas without additional configuration, alertmanager is responsible for alert deduplication.


## Alertmanager
The final step of the high availability scheme is Alertmanager, when an alert triggers, actually fires alerts against *all* instances of an Alertmanager cluster. 

The Alertmanager, starting with the `v0.5.0` release, ships with a high availability mode. It implements a gossip protocol to synchronize instances of an Alertmanager cluster regarding notifications that have been sent out, to prevent duplicate notifications. It is an AP (available and partition tolerant) system. Being an AP system, means that notifications are guaranteed to be sent at least once. 

The Victoria Metrics Operator ensures that Alertmanager clusters are properly configured to run highly available on Kubernetes.


## VmSingle

 It doesnt support high availability, for such purpose use VmCluster. It will be introduced later.
