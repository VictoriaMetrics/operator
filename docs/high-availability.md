---
sort: 8
weight: 8
title: High Availability
---

# High Availability

High availability is not only important for customer-facing software but if the monitoring infrastructure is not highly available, then there is a risk that operations people are not notified of alerts.
Therefore, high availability must be just as thought through for the monitoring stack, as for anything else.

VictoriaMetrics operator support high availability for each component of the monitoring stack:

- [VMCluster](https://docs.victoriametrics.com/operator/resources/vmcluster.html#high-availability)
- [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html#high-availability)
- [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html#high-availability)
- [VMAlertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html#high-availability)
- [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth.html#high-availability)

<!-- TODO: HA for operator -->
