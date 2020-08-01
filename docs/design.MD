# Design

This document describes the design and interaction between the custom resource definitions that the Victoria Metrics Operator introduces.

The custom resources that the Prometheus Operator introduces are:

* [VMSingle](#VMSingle)
* [VMCluster](#VMCluster)
* [VMAgent](#VMAgent)
* [VMAlert](#VMAlert)
* [VMServiceScrape](#VMServiceScrape)
* [VMPodScrape](#VMPodScrape)
* [Alertmanager](#Alertmanager)
* [VMRule](#VMRule)

## VMSingle

The `VMSingle` custom resource definition (CRD) declaratively defines a desired VMSingle setup to run in a Kubernetes cluster. It provides options to configure its parameters. 

For each `VMSingle` resource, the Operator deploys a properly configured `Deployment` in the same namespace. The VMSingle `Pod`s are configured to mount an empty dir or  `PersistentVolumeClaimSpec` for storing metrics data. Deployment update strategy set to `ReCreate`. You cannot specify more than one replica size.

For each `VMSingle` resource, the Operator adds  `Service` in the same namespace and `VMServiceScrape`. it uses prefixed name `<VMSingle-name>`

## VMCluster
 
The `VMCluster` custom resource definition (CRD) defines a cluster of VictoriaMetrics database [url](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/cluster). 

For each `VMCluster`  resource, the operator creates `VMStorage` as `StatefulSet`, `VMSelect` as `StatefulSet` and `VMInsert` as deployment. For `VMStorage` and `VMSelect` headless  services are created, for `VMInsert` is created service  with clusterIP. 

There is strict order for these objects creation and reconciliation. First `VMStorage` is synced - the operator waits until all its pod will become ready, then it syncs `VMSelect` with the same manner. `VMInsert` is the last object for sync.

All statefulsets are created with `OnDelete` update type. It allows the operator to manually manage the rolling update process - by deleting one by one pod and waiting for its ready status.

Rolling update process may be configured by operator env variables - the most important is `VM_PODWAITREADYTIMEOUT=80s` - it controls how long to wait until pod ready status.

## VMAgent

The `VMAgent` custom resource definition (CRD) declaratively defines a desired VMAgent setup to run in a Kubernetes cluster. It provides options to configure its parameters. 

For each `VMAgent` resource, the Operator deploys a properly configured `Deployment` in the same namespace. The VMAgent `Pod`s are configured to mount a `Secret` called `<VMAgent-name>` containing the configuration for VMAgent.

For each `VMAgent` resource, the Operator adds  `Service` in the same namespace and `VMServiceScrape`. it uses prefixed name `<VMAgent-name>`

The CRD specifies which `VMServiceScrape`s should be covered by the deployed VMAgent instances based on label selection. The Operator then generates a configuration based on the included `VMServiceScrape`s and updates it in the `Secret` containing the configuration. It continuously does so for all changes that are made to `VMServiceScrape`s or the `VMAgent` resource itself.

If no selection of `VMServiceScrape`s is provided, the Operator leaves management of the `Secret` to the user, which allows to provide custom configurations while still benefiting from the Operator's capabilities of managing VMAgent setups.

## VMAlert

The `VMAlert` custom resource definition (CRD) declaratively defines a desired VMAlert setup to run in a Kubernetes cluster. It provides options to configure its parameters. 

For each `VMAlert` resource, the Operator deploys a properly configured `Deployment` in the same namespace. The VMAlert `Pod`s are configured to mount a list of `Configmaps` called `<VMAlert-name>-number` containing the configuration for alerting rules.

For each `VMAlert` resource, the Operator adds  `Service` in the same namespace and `VMServiceScrape`. it uses prefixed name `<VMAlert-name>`

The CRD specifies which `VMRule`s should be covered by the deployed VMAlert instances based on label selection. The Operator then generates a configuration based on the included `VMRule`s and updates it in the `Configmaps` containing the configuration. It continuously does so for all changes that are made to `VMRule`s or the `VMAlert` resource itself.

Alerting rules are filtered by selector `ruleNamespaceSelector` in `VMAlert` crd definition, for selecting rules from all namespaces you must specify it to empty value:

```yaml
spec:
 ruleNamespaceSelector: {}
```


## VMServiceScrape

The `VMServiceScrape` custom resource definition (CRD) allows to declaratively define how a dynamic set of services should be monitored. Which services are selected to be monitored with the desired configuration is defined using label selections. This allows an organization to introduce conventions around how metrics are exposed and then following these conventions new services are automatically discovered, without the need to reconfigure the system.

For VMAgent to monitor any application within Kubernetes an `Endpoints` object needs to exist. `Endpoints` objects are essentially lists of IP addresses. Typically an `Endpoints` object is populated by a `Service` object. A `Service` object discovers `Pod`s by a label selector and adds those to the `Endpoints` object.

A `Service` may expose one or more service ports, which are backed by a list of multiple endpoints that point to a `Pod` in the common case. This is reflected in the respective `Endpoints` object as well.

The `VMServiceScrape` object introduced by the Victoria Metrics Operator, in turn, discovers those `Endpoints` objects and configures VMAgent to monitor those `Pod`s.

The `endpoints` section of the `VMServiceScrapeSpec`, is used to configure which ports of these `Endpoints` are going to be scraped for metrics, and with which parameters. For advanced use cases, one may want to monitor ports of backing `Pod`s, which are not directly part of the service endpoints. Therefore when specifying an endpoint in the `endpoints` section, they are strictly used.

> Note: `endpoints` (lowercase) is the field in the `VMServiceScrape` CRD, while `Endpoints` (capitalized) is the Kubernetes object kind.

Both `VMServiceScrape`, as well as discovered targets, may come from any namespace. This is important to allow cross-namespace monitoring use cases, e.g. for meta-monitoring. Using the `VMServiceScrape` of the `VMAgentSpec`, one can restrict the namespaces `VMServiceScrape`s are selected from by the respective VMAgent server. Using the `namespaceSelector` of the `VMServiceScrape`, one can restrict the namespaces the `Endpoints` objects are allowed to be discovered from.
To discover targets in all namespaces the `namespaceSelector` has to be empty:

```yaml
spec:
  namespaceSelector: {}
```

## VMPodScrape

The `VMPodScrape` custom resource definition (CRD) allows to declaratively define how a dynamic set of pods should be monitored.
Which pods are selected to be monitored with the desired configuration is defined using label selections.
This allows an organization to introduce conventions around how metrics are exposed and then following these conventions new pods are automatically discovered, without the need to reconfigure the system.

A `Pod` is a collection of one or more containers which can expose Prometheus metrics on a number of ports.

The `VMPodScrape` object introduced by the Victoria Metrics Operator discovers these pods and generates the relevant configuration for the Prometheus server in order to monitor them. 

The `PodMetricsEndpoints` section of the `VMPodScrapeSpec`, is used to configure which ports of a pod are going to be scraped for metrics, and with which parameters.

Both `VMPodScrapes`, as well as discovered targets, may come from any namespace. This is important to allow cross-namespace monitoring use cases, e.g. for meta-monitoring.
Using the `namespaceSelector` of the `VMPodScrapeSpec`, one can restrict the namespaces the `Pods` are allowed to be discovered from.
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

## VMRule

The `VMRule` CRD declaratively defines a desired Prometheus rule to be consumed by one or more VMAlert instances. 

Alerts and recording rules can be saved and applied as YAML files, and dynamically loaded without requiring any restart.


## VMPrometheusConverter


 By default, the operator converts and updates existing prometheus-operator API objects:
 
 `ServiceMonitor` into `VMServiceScrape`
 `PodMonitor` into `VMPodScrape`
 `PrometheusRule` into `VMRule`
 
 Removing prometheus-operator API objects wouldn't delete converted objects. So you can safely migrate or run two operators in parallel mode.
 
  
