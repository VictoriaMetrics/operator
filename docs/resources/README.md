---
sort: 14
weight: 14
title: Custom resources
---

#  Custom resource kinds

This documentation section describes the design and interaction between the custom resource definitions (CRD) that the Victoria
Metrics Operator introduces.

[Operator]((https://docs.victoriametrics.com/operator/)) introduces the
following [custom resources](https://docs.victoriametrics.com/operator/#custom-resources):

- [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html)
- [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html)
- [VMAlertManager](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html)
- [VMAlertManagerConfig](https://docs.victoriametrics.com/operator/resources/vmalertmanagerconfig.html)
- [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth.html)
- [VMCluster](https://docs.victoriametrics.com/operator/resources/vmcluster.html)
- [VMNodeScrape](https://docs.victoriametrics.com/operator/resources/vmnodescrape.html)
- [VMPodScrape](https://docs.victoriametrics.com/operator/resources/vmpodscrape.html)
- [VMProbe](https://docs.victoriametrics.com/operator/resources/vmprobe.html)
- [VMRule](https://docs.victoriametrics.com/operator/resources/vmrule.html)
- [VMServiceScrape](https://docs.victoriametrics.com/operator/resources/vmservicescrape.html)
- [VMStaticScrape](https://docs.victoriametrics.com/operator/resources/vmstaticscrape.html)
- [VMSingle](https://docs.victoriametrics.com/operator/resources/vmsingle.html)
- [VMUser](https://docs.victoriametrics.com/operator/resources/vmuser.html)

Here is the scheme of relations between the custom resources:

<img src="README_cr-relations.png">

## Specification

You can find the specification for the custom resources on **[API Docs](https://docs.victoriametrics.com/operator/api.html)**.

### Extra arguments

If you can't find necessary field in the specification of custom resource, 
you can use `extraArgs` field for passing additional arguments to the application.

Field `extraArgs` is supported for the following custom resources:

- [VMAgent spec](https://docs.victoriametrics.com/operator/api.html#vmagentspec)
- [VMAlert spec](https://docs.victoriametrics.com/operator/api.html#vmalertspec)
- [VMAlertManager spec](https://docs.victoriametrics.com/operator/api.html#vmalertmanagerspec)
- [VMAuth spec](https://docs.victoriametrics.com/operator/api.html#vmauthspec)
- [VMCluster/vmselect spec](https://docs.victoriametrics.com/operator/api.html#vmselect)
- [VMCluster/vminsert spec](https://docs.victoriametrics.com/operator/api.html#vminsert)
- [VMCluster/vmstorage spec](https://docs.victoriametrics.com/operator/api.html#vmstorage)
- [VMSingle spec](https://docs.victoriametrics.com/operator/api.html#vmsinglespec)

Supported flags for each application can be found the in the corresponding documentation:

- [VMAgent flags](https://docs.victoriametrics.com/vmagent.html#advanced-usage)
- [VMAlert](https://docs.victoriametrics.com/vmalert.html#configuration)
- [VMAuth](https://docs.victoriametrics.com/vmauth.html#advanced-usage)
- [VMCluster](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#list-of-command-line-flags)
- [VMSingle](https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html#list-of-command-line-flags)

Usage example:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-example-exrtaargs
spec:
  retentionPeriod: "1"
  extraArgs:
    dedup.minScrapeInterval: 60s
  # ...
```

### Extra environment variables

Flag can be replaced with environment variable, it's useful for retrieving value from secret. 
You can use `extraEnvs` field for passing additional arguments to the application.

Usage example:

```yaml
kind: VMSingle
metadata:
  name: vmsingle-example--exrtaenvs
spec:
  retentionPeriod: "1"
  extraEnvs:
    - name: DEDUP_MINSCRAPEINTERVAL
      valueFrom:
        secretKeyRef:
          name: vm-secret
          key: dedup
```

This feature really useful for using with 
[`-envflag.enable` command-line argument](https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html#environment-variables).

## Examples

Page for every custom resource contains examples section:

- [VMAgent examples](https://docs.victoriametrics.com/operator/resources/vmagent.html#examples)
- [VMAlert examples](https://docs.victoriametrics.com/operator/resources/vmalert.html#examples)
- [VMAlertmanager examples](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html#examples)
- [VMAlertmanagerConfig examples](https://docs.victoriametrics.com/operator/resources/vmalertmanagerconfig.html#examples)
- [VMAuth examples](https://docs.victoriametrics.com/operator/resources/vmauth.html#examples)
- [VMCluster examples](https://docs.victoriametrics.com/operator/resources/vmcluster.html#examples)
- [VMNodeScrape examples](https://docs.victoriametrics.com/operator/resources/vmnodescrape.html#examples)
- [VMPodScrape examples](https://docs.victoriametrics.com/operator/resources/vmpodscrape.html#examples)
- [VMProbe examples](https://docs.victoriametrics.com/operator/resources/vmprobe.html#examples)
- [VMRule examples](https://docs.victoriametrics.com/operator/resources/vmrule.html#examples)
- [VMServiceScrape examples](https://docs.victoriametrics.com/operator/resources/vmservicescrape.html#examples)
- [VMStaticScrape examples](https://docs.victoriametrics.com/operator/resources/vmstaticscrape.html#examples)
- [VMSingle examples](https://docs.victoriametrics.com/operator/resources/vmsingle.html#examples)
- [VMUser examples](https://docs.victoriametrics.com/operator/resources/vmuser.html#examples)

In addition, you can find examples of the custom resources for VIctoriMetrics operator in 
the **[examples directory](https://github.com/VictoriaMetrics/operator/tree/master/config/examples) of operator repository**.

## Managing versions of VM

Every custom resource with deployable application has a fields for specifying version (docker image) of component:

- [Managing versions for VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html#version-management)
- [Managing versions for VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html#version-management)
- [Managing versions for VMAlertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html#version-management)
- [Managing versions for VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth.html#version-management)
- [Managing versions for VMCluster](https://docs.victoriametrics.com/operator/resources/vmcluster.html#version-management)
- [Managing versions for VMSingle](https://docs.victoriametrics.com/operator/resources/vmsingle.html#version-management)

## High availability

VictoriaMetrics operator support high availability for each component of the monitoring stack:

- [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html#high-availability)
- [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html#high-availability)
- [VMAlertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html#high-availability)
- [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth.html#high-availability)
- [VMCluster](https://docs.victoriametrics.com/operator/resources/vmcluster.html#high-availability)

In addition, these CRD support common features, that can be used to increase high availability - resources above have the following fields:

- `affinity` - to schedule pods on different nodes ([affinity and anti-affinity in kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity)),
- `tolerations` - to schedule pods on nodes with taints ([taints and tolerations in kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/)),
- `nodeSelector` - to schedule pods on nodes with specific labels ([node selector in kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector)),
- `topologySpreadConstraints` - to schedule pods on different nodes in the same topology ([topology spread constraints in kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#pod-topology-spread-constraints)).

See details about these fields in the [Specification](#specification).

## Enterprise features

Operator supports following [Enterprise features for VictoriaMetrics components](https://docs.victoriametrics.com/enterprise.html):

- [VMAgent Enterprise features](https://docs.victoriametrics.com/operator/resources/vmagent.html#enterprise-features):
    - [Reading metrics from kafka](https://docs.victoriametrics.com/operator/resources/vmagent.html#reading-metrics-from-kafka)
    - [Writing metrics to kafka](https://docs.victoriametrics.com/operator/resources/vmagent.html#writing-metrics-to-kafka)
- [VMAlert Enterprise features](https://docs.victoriametrics.com/operator/resources/vmalert.html#enterprise-features):
    - [Reading rules from object storage](https://docs.victoriametrics.com/operator/resources/vmalert.html#reading-rules-from-object-storage)
    - [Multitenancy](https://docs.victoriametrics.com/operator/resources/vmalert.html#multitenancy)
- [VMAuth Enterprise features](https://docs.victoriametrics.com/operator/resources/vmauth.html#enterprise-features)
    - [IP Filters](https://docs.victoriametrics.com/operator/resources/vmauth.html#ip-filters)
- [VMCluster Enterprise features](https://docs.victoriametrics.com/operator/resources/vmcluster.html#enterprise-features)
    - [Downsampling](https://docs.victoriametrics.com/operator/resources/vmcluster.html#downsampling)
    - [Multiple retentions / Retention filters](https://docs.victoriametrics.com/operator/resources/vmcluster.html#retention-filters)
    - [Advanced per-tenant statistic](https://docs.victoriametrics.com/operator/resources/vmcluster.html#advanced-per-tenant-statistic)
    - [mTLS protection](https://docs.victoriametrics.com/operator/resources/vmcluster.html#mtls-protection)
    - [Backup atomation](https://docs.victoriametrics.com/operator/resources/vmcluster.html#backup-atomation)
- [VMRule Enterprise features](https://docs.victoriametrics.com/operator/resources/vmrule.html#enterprise-features)
    - [Multitenancy](https://docs.victoriametrics.com/operator/resources/vmrule.html#multitenancy)
- [VMSingle Enterprise features](https://docs.victoriametrics.com/operator/resources/vmsingle.html#enterprise-features)
    - [Downsampling](https://docs.victoriametrics.com/operator/resources/vmsingle.html#downsampling)
    - [Retention filters](https://docs.victoriametrics.com/operator/resources/vmsingle.html#retention-filters)
    - [Backup atomation](https://docs.victoriametrics.com/operator/resources/vmsingle.html#backup-atomation)
- [VMUser Enterprise features](https://docs.victoriametrics.com/operator/resources/vmuser.html#enterprise-features)
    - [IP Filters](https://docs.victoriametrics.com/operator/resources/vmuser.html#ip-filters)

More information about enterprise features you can read
on [VictoriaMetrics Enterprise page](https://docs.victoriametrics.com/enterprise.html#victoriametrics-enterprise).

## Configuration synchronization

### Basic concepts

VictoriaMetrics applications, like many other applications with configuration file deployed at Kubernetes, uses `ConfigMaps` and `Secrets` for configuration files.
Usually, it's out of application scope to watch for configuration on-disk changes.
Applications reload their configuration by a signal from a user or some other tool, that knows how to watch for updates.
At Kubernetes, the most popular design for this case is a sidecar container, that watches for configuration file changes and sends an HTTP request to the application.

`Configmap` or `Secret` that mounted at `Pod` holds a copy of its content.
Kubernetes component `kubelet` is responsible for content synchronization between an object at Kubernetes API and a file served on disk.
It's not efficient to sync its content immediately, and `kubelet` eventually synchronizes it. There is a configuration option, that controls this period.

That's why, applications managed by operator don't receive changes immediately. It usually takes 1-2 min, before content will be updated.

It may trigger errors when an application was deleted, but [`VMAgent`](https://docs.victoriametrics.com/operator/resources/vmagent) still tries to scrape it.

### Possible mitigations

The naive solution for this case decrease the synchronization period. But it configures globally and may be hard for operator users.

That's why operator uses a few hacks.

For `ConfigMap` updates, operator changes annotation with a time of `Configmap` content update. It triggers `ConfigMap`'s content synchronization by kubelet immediately.
It's the case for `VMAlert`, it uses `ConfigMap` as a configuration source.

For `Secret` it doesn't work. And operator offers its implementation for side-car container. It can be configured with env variable for operator:

```
- name: VM_USECUSTOMCONFIGRELOADER
  value: "true"
```

If it's defined, operator uses own [config-reloader](https://github.com/VictoriaMetrics/operator/tree/master/internal/config-reloader)
instead of [prometheus-config-reload](https://github.com/prometheus-operator/prometheus-operator/tree/main/cmd/prometheus-config-reloader).

It watches corresponding `Secret` for changes with Kubernetes API watch call and writes content into emptyDir.
This emptyDir shared with the application.
In case of content changes, `config-reloader` sends HTTP requests to the application.
It greatly reduces the time for configuration synchronization.
