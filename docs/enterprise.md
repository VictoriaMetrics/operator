---
weight: 13
title: Enterprise features
menu:
  docs:
    parent: "operator"
    weight: 13
aliases:
  - /operator/enterprise/
  - /operator/enterprise/index.html
tags:
  - kubernetes
  - logs
  - metrics
---
Operator doesn't have enterprise version for itself, but it supports 
[enterprise features for VictoriaMetrics components](https://docs.victoriametrics.com/victoriametrics/enterprise/):

- [VMAgent Enterprise features](https://docs.victoriametrics.com/operator/resources/vmagent/#enterprise-features):
  - [Reading metrics from kafka](https://docs.victoriametrics.com/operator/resources/vmagent/#reading-metrics-from-kafka)
  - [Writing metrics to kafka](https://docs.victoriametrics.com/operator/resources/vmagent/#writing-metrics-to-kafka)
- [VMAlert Enterprise features](https://docs.victoriametrics.com/operator/resources/vmalert/#enterprise-features):
  - [Reading rules from object storage](https://docs.victoriametrics.com/operator/resources/vmalert/#reading-rules-from-object-storage)
  - [Multitenancy](https://docs.victoriametrics.com/operator/resources/vmalert/#multitenancy)
- [VMAuth Enterprise features](https://docs.victoriametrics.com/operator/resources/vmauth/#enterprise-features)
  - [IP Filters](https://docs.victoriametrics.com/operator/resources/vmauth/#ip-filters) 
- [VMCluster Enterprise features](https://docs.victoriametrics.com/operator/resources/vmcluster/#enterprise-features)
  - [Downsampling](https://docs.victoriametrics.com/operator/resources/vmcluster/#downsampling)
  - [Multiple retentions / Retention filters](https://docs.victoriametrics.com/operator/resources/vmcluster/#retention-filters)
  - [Advanced per-tenant statistic](https://docs.victoriametrics.com/operator/resources/vmcluster/#advanced-per-tenant-statistic)
  - [mTLS protection](https://docs.victoriametrics.com/operator/resources/vmcluster/#mtls-protection)
  - [Backup automation](https://docs.victoriametrics.com/operator/resources/vmcluster/#backup-automation)
- [VMRule Enterprise features](https://docs.victoriametrics.com/operator/resources/vmrule/#enterprise-features)
  - [Multitenancy](https://docs.victoriametrics.com/operator/resources/vmrule/#multitenancy)
- [VMSingle Enterprise features](https://docs.victoriametrics.com/operator/resources/vmsingle/#enterprise-features)
  - [Downsampling](https://docs.victoriametrics.com/operator/resources/vmsingle/#downsampling)
  - [Retention filters](https://docs.victoriametrics.com/operator/resources/vmsingle/#retention-filters)
  - [Backup automation](https://docs.victoriametrics.com/operator/resources/vmsingle/#backup-automation)
- [VMUser Enterprise features](https://docs.victoriametrics.com/operator/resources/vmuser/#enterprise-features)
  - [IP Filters](https://docs.victoriametrics.com/operator/resources/vmuser/#ip-filters) 

More information about enterprise features you can read 
on [VictoriaMetrics Enterprise page](https://docs.victoriametrics.com/victoriametrics/enterprise/#victoriametrics-enterprise-features).

In order to find examples of deploying enterprise components with operator,
please, check [this](https://docs.victoriametrics.com/victoriametrics/enterprise/#kubernetes-operator) documentation.

## License and ESA

Enterprise features are enabled with the [`spec.license`](https://docs.victoriametrics.com/operator/api/#v1beta1-license) field
of the custom resource and an enterprise version of the component.
The operator passes `spec.license.key` to the component as the `-license` command-line flag,
and mounts the secret from `spec.license.keyRef` and passes it as the `-licenseFile` flag.

Starting from VictoriaMetrics [v1.156.0](https://github.com/VictoriaMetrics/VictoriaMetrics/releases/tag/v1.156.0),
VictoriaMetrics Enterprise components are started with the `-eula` flag to accept the [ESA](https://victoriametrics.com/legal/esa/),
and license keys are no longer verified. The `-license`, `-licenseFile`, `-licenseFile.reloadInterval` and `-license.forceOffline`
flags are deprecated and treated as `-eula`, so existing `spec.license` settings keep working without changes.
See [Running VictoriaMetrics Enterprise](https://docs.victoriametrics.com/victoriametrics/enterprise/) for details.

This applies to `VMAgent`, `VMAlert`, `VMAuth`, `VMCluster`, `VMSingle` and the components they manage, such as `vmbackupmanager`.
For these resources running v1.156.0 or newer:

- `spec.license.key` or `spec.license.keyRef` is still required by the operator to enable enterprise features,
  such as downsampling, retention filters, automatic vmstorage discovery and backup automation.
  Since the key is no longer verified, any non-empty value in `spec.license.key` accepts the ESA.
  The license is shared by all components of the resource, including `vmbackupmanager`:

  ```yaml
  spec:
    license:
      # any non-empty value works, it doesn't have to be a valid license key
      key: accept-esa
  ```

- `spec.license.forceOffline` and `spec.license.reloadInterval` have no effect.
- `spec.license.keyRef` must point to an existing Secret, because the operator mounts it into the pods.
