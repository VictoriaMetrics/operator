[![Latest Release](https://img.shields.io/github/release/VictoriaMetrics/operator.svg?style=flat-square)](https://github.com/VictoriaMetrics/operator/releases/latest)
[![Docker Pulls](https://img.shields.io/docker/pulls/victoriametrics/operator.svg?maxAge=604800)](https://hub.docker.com/r/victoriametrics/operator)
[![Slack](https://img.shields.io/badge/join%20slack-%23victoriametrics-brightgreen.svg)](http://slack.victoriametrics.com/)
[![GitHub license](https://img.shields.io/github/license/VictoriaMetrics/operator.svg)](https://github.com/VictoriaMetrics/operator/blob/master/LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/VictoriaMetrics/operator)](https://goreportcard.com/report/github.com/VictoriaMetrics/operator)
[![Build Status](https://github.com/VictoriaMetrics/VictoriaMetrics/workflows/main/badge.svg)](https://github.com/VictoriaMetrics/operator/actions)

![Victoria Metrics logo](logo.png "Victoria Metrics")

# VictoriaMetrics operator

## Overview

 Design and implementation inspired by [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator). It's great a tool for managing monitoring configuration of your applications. VictoriaMetrics operator has api capability with it.
So you can use familiar CRD objects: `ServiceMonitor`, `PodMonitor`, `PrometheusRule` and `Probe`. Or you can use VictoriaMetrics CRDs:
- `VMServiceScrape` - defines scraping metrics configuration from pods backed by services.
- `VMPodScrape` - defines scraping metrics configuration from pods.
- `VMRule` - defines alerting or recording rules.
- `VMProbe` - defines a probing configuration for targets with blackbox exporter.

Besides, operator allows you to manage VictoriaMetrics applications inside kubernetes cluster and simplifies this process [quick-start](/docs/quick-start.MD)
With CRD (Custom Resource Definition) you can define application configuration and apply it to your cluster [crd-objects](/docs/api.MD).

 Operator simplifies VictoriaMetrics cluster installation, upgrading and managing.

 It has integration with VictoriaMetrics `vmbackupmanager` - advanced tools for making backups. Check backup [docs](/docs/backups.MD)

## Use cases

 For kubernetes-cluster administrators, it simplifies installation, configuration, management for `VictoriaMetrics` application. And the main feature of operator -  is ability to delegate applications monitoring configuration to the end-users.

 For applications developers, its great possibility for managing observability of applications. You can define metrics scraping and alerting configuration for your application and manage it with an application deployment process. Just define app_deployment.yaml, app_vmpodscrape.yaml and app_vmrule.yaml. That's it, you can apply it to a kubernetes cluster. Check [quick-start](/docs/quick-start.MD) for an example.

## Operator vs helm-chart

VictoriaMetrics provides [helm charts](https://github.com/VictoriaMetrics/helm-charts). Operator makes the same, simplifies it and provides advanced features.

## Documentation

- quick start [doc](https://docs.victoriametrics.com/operator/quick-start.html)
- high availability [doc](https://docs.victoriametrics.com/operator/high-availability.html)
- relabeling configuration [doc](https://docs.victoriametrics.com/operator/relabeling.html)
- managing crd objects versions [doc](https://docs.victoriametrics.com/operator/managing-versions.html)
- design and description of implementation [design](https://docs.victoriametrics.com/operator/design.html)
- operator objects description [doc](https://docs.victoriametrics.com/operator/api.html)
- backups [docs](https://docs.victoriametrics.com/operator/backups.html)
- external access to cluster resources[doc](https://docs.victoriametrics.com/operator/auth.html)
- security [doc](https://docs.victoriametrics.com/operator/security.html)
- resource validation [doc](https://docs.victoriametrics.com/operator/resources-validation.html)

  NOTE documentations was moved into main VictoriaMetrics repo [link](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/docs/operator)
  All changes must be done there.

## Configuration

 Operator configured by env variables, list of it can be found at [link](https://docs.victoriametrics.com/operator/vars.html)

 It defines default configuration options, like images for components, timeouts, features.


## Kubernetes' compatibility versions

operator tested at kubernetes versions
from 1.16 to 1.22

 For clusters version below 1.16 you must use legacy CRDs from [path](config/crd/legacy)
  and disable CRD controller with flag: `--controller.disableCRDOwnership=true`

## Troubleshooting

- cannot apply crd at kubernetes 1.18 + version and kubectl reports error:
```bash
Error from server (Invalid): error when creating "release/crds/crd.yaml": CustomResourceDefinition.apiextensions.k8s.io "vmalertmanagers.operator.victoriametrics.com" is invalid: [spec.validation.openAPIV3Schema.properties[spec].properties[initContainers].items.properties[ports].items.properties[protocol].default: Required value: this property is in x-kubernetes-list-map-keys, so it must have a default or be a required property, spec.validation.openAPIV3Schema.properties[spec].properties[containers].items.properties[ports].items.properties[protocol].default: Required value: this property is in x-kubernetes-list-map-keys, so it must have a default or be a required property]
Error from server (Invalid): error when creating "release/crds/crd.yaml": CustomResourceDefinition.apiextensions.k8s.io "vmalerts.operator.victoriametrics.com" is invalid: [
```
  upgrade to the latest release version. There is a bug with kubernetes objects at the early releases.

## Community and contributions

Feel free asking any questions regarding VictoriaMetrics:

* [slack](http://slack.victoriametrics.com/)
* [reddit](https://www.reddit.com/r/VictoriaMetrics/)
* [telegram-en](https://t.me/VictoriaMetrics_en)
* [telegram-ru](https://t.me/VictoriaMetrics_ru1)
* [google groups](https://groups.google.com/forum/#!forum/victorametrics-users)


## Development

- operator-sdk verson v1.0.0 +  [https://github.com/operator-framework/operator-sdk]
- golang 1.15 +
- minikube or kind

start:
```bash
make run
```

for test execution run:
```bash
#unit tests

make test

# you need minikube or kind for e2e, do not run it on live cluster
#e2e tests with local binary
make e2e-local
```
