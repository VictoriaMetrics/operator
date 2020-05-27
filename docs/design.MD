# Design

This document describes the design and interaction between the custom resource definitions that the Victoria Metrics Operator introduces.

The custom resources that the Prometheus Operator introduces are:

* `VmAgent`
* `VmAlert`
* `VmSingle`
* `ServiceMonitor`
* `PodMonitor` 
* `VmAlertmanager`
* `PrometheusRule`

## VmAgent

The `VmAgent` custom resource definition (CRD) declaratively defines a desired VmAgent setup to run in a Kubernetes cluster. It provides options to configure it parameters. 

For each `VmAgent` resource, the Operator deploys a properly configured `Deployment` in the same namespace. The VmAgent `Pod`s are configured to mount a `Secret` called `<vmagent-name>` containing the configuration for VmAgent.

For each `VmAgent` resource, the Operator adds  `Service` in the same namespace and `ServiceMonitor`. it uses prefixed name `<vmagent-name>`

The CRD specifies which `ServiceMonitor`s should be covered by the deployed VmAgent instances based on label selection. The Operator then generates a configuration based on the included `ServiceMonitor`s and updates it in the `Secret` containing the configuration. It continuously does so for all changes that are made to `ServiceMonitor`s or the `VmAgent` resource itself.

If no selection of `ServiceMonitor`s is provided, the Operator leaves management of the `Secret` to the user, which allows to provide custom configurations while still benefiting from the Operator's capabilities of managing VmAgent setups.

## VmAlert

The `VmAlert` custom resource definition (CRD) declaratively defines a desired VmAlert setup to run in a Kubernetes cluster. It provides options to configure it parameters. 

For each `VmAlert` resource, the Operator deploys a properly configured `Deployment` in the same namespace. The VmAlert `Pod`s are configured to mount a list of `Configmaps` called `<vmalert-name>-number` containing the configuration for alerting rules.

For each `VmAlert` resource, the Operator adds  `Service` in the same namespace and `ServiceMonitor`. it uses prefixed name `<vmalert-name>`

The CRD specifies which `PrometheusRule`s should be covered by the deployed VmAlert instances based on label selection. The Operator then generates a configuration based on the included `PrometheusRule`s and updates it in the `Configmaps` containing the configuration. It continuously does so for all changes that are made to `PrometheusRule`s or the `Vmalert` resource itself.

Alerting rules are filtered by selector `ruleNamespaceSelector` in `VmAlert` crd definition, for selecting rules from all namespaces you must specify it to empty value:

 ```yaml
spec:
 ruleNamespaceSelector: {}
```

## VmSingle

The `VmSingle` custom resource definition (CRD) declaratively defines a desired VmSingle setup to run in a Kubernetes cluster. It provides options to configure it parameters. 

For each `VmSingle` resource, the Operator deploys a properly configured `Deployment` in the same namespace. The VmSingle `Pod`s are configured to mount a emtpy dir or  `PersistentVolumeClaimSpec` for storing metrics data. Deployment update strategy set to `ReCreate`. You cannot specify more then 1 replica size.

For each `VmSingle` resource, the Operator adds  `Service` in the same namespace and `ServiceMonitor`. it uses prefixed name `<vmsingle-name>`


## ServiceMonitor

The `ServiceMonitor` custom resource definition (CRD) allows to declaratively define how a dynamic set of services should be monitored. Which services are selected to be monitored with the desired configuration is defined using label selections. This allows an organization to introduce conventions around how metrics are exposed, and then following these conventions new services are automatically discovered, without the need to reconfigure the system.

For VmAgent to monitor any application within Kubernetes an `Endpoints` object needs to exist. `Endpoints` objects are essentially lists of IP addresses. Typically an `Endpoints` object is populated by a `Service` object. A `Service` object discovers `Pod`s by a label selector and adds those to the `Endpoints` object.

A `Service` may expose one or more service ports, which are backed by a list of multiple endpoints that point to a `Pod` in the common case. This is reflected in the respective `Endpoints` object as well.

The `ServiceMonitor` object introduced by the Victoria Metrics Operator in turn discovers those `Endpoints` objects and configures VmAgent to monitor those `Pod`s.

The `endpoints` section of the `ServiceMonitorSpec`, is used to configure which ports of these `Endpoints` are going to be scraped for metrics, and with which parameters. For advanced use cases one may want to monitor ports of backing `Pod`s, which are not directly part of the service endpoints. Therefore when specifying an endpoint in the `endpoints` section, they are strictly used.

> Note: `endpoints` (lowercase) is the field in the `ServiceMonitor` CRD, while `Endpoints` (capitalized) is the Kubernetes object kind.

Both `ServiceMonitors` as well as discovered targets may come from any namespace. This is important to allow cross-namespace monitoring use cases, e.g. for meta-monitoring. Using the `ServiceMonitorNamespaceSelector` of the `VmAgentSpec`, one can restrict the namespaces `ServiceMonitor`s are selected from by the respective VmAgent server. Using the `namespaceSelector` of the `ServiceMonitorSpec`, one can restrict the namespaces the `Endpoints` objects are allowed to be discovered from.
To discover targets in all namespaces the `namespaceSelector` has to be empty:
```yaml
spec:
  namespaceSelector: {}
```

## PodMonitor

The `PodMonitor` custom resource definition (CRD) allows to declaratively define how a dynamic set of pods should be monitored.
Which pods are selected to be monitored with the desired configuration is defined using label selections.
This allows an organization to introduce conventions around how metrics are exposed, and then following these conventions new pods are automatically discovered, without the need to reconfigure the system.

A `Pod` is a collection of one or more containers which can expose Prometheus metrics on a number of ports.

The `PodMonitor` object introduced by the Victoria Metrics Operator discovers these pods and generates the relevant configuration for the Prometheus server in order to monitor them. 

The `PodMetricsEndpoints` section of the `PodMonitorSpec`, is used to configure which ports of a pod are going to be scraped for metrics, and with which parameters.

Both `PodMonitors` as well as discovered targets may come from any namespace. This is important to allow cross-namespace monitoring use cases, e.g. for meta-monitoring.
Using the `namespaceSelector` of the `PodMonitorSpec`, one can restrict the namespaces the `Pods` are allowed to be discovered from.
To discover targets in all namespaces the `namespaceSelector` has to be empty:
```yaml
spec:
  namespaceSelector:
    any: true
```

## Alertmanager

The `Alertmanager` custom resource definition (CRD) declaratively defines a desired Alertmanager setup to run in a Kubernetes cluster. It provides options to configure replication and persistent storage.

For each `Alertmanager` resource, the Operator deploys a properly configured `StatefulSet` in the same namespace. The Alertmanager pods are configured to include a `Secret` called `<alertmanager-name>` which holds the used configuration file in the key `alertmanager.yaml`.

When there are two or more configured replicas the operator runs the Alertmanager instances in high availability mode.

## PrometheusRule

The `PrometheusRule` CRD declaratively defines a desired Prometheus rule to be consumed by one or more VmAlert instances. 

Alerts and recording rules can be saved and applied as YAML files, and dynamically loaded without requiring any restart.
