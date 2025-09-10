[![Latest Release](https://img.shields.io/github/release/VictoriaMetrics/operator.svg?style=flat-square)](https://github.com/VictoriaMetrics/operator/releases/latest)
[![Docker Pulls](https://img.shields.io/docker/pulls/victoriametrics/operator.svg?maxAge=604800)](https://hub.docker.com/r/victoriametrics/operator)
[![Slack](https://img.shields.io/badge/join%20slack-%23victoriametrics-brightgreen.svg)](http://slack.victoriametrics.com/)
[![GitHub license](https://img.shields.io/github/license/VictoriaMetrics/operator.svg)](https://github.com/VictoriaMetrics/operator/blob/master/LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/VictoriaMetrics/operator)](https://goreportcard.com/report/github.com/VictoriaMetrics/operator)
[![Build and Test Status](https://github.com/VictoriaMetrics/operator/actions/workflows/main.yaml/badge.svg)](https://github.com/VictoriaMetrics/operator/actions)

<picture>
  <source srcset="docs/logo_white.webp" media="(prefers-color-scheme: dark)">
  <source srcset="docs/logo.webp" media="(prefers-color-scheme: light)">
  <img src="docs/logo.webp" width="300" alt="VictoriaMetrics logo">
</picture>

# VictoriaMetrics operator

## Overview

 Design and implementation inspired by [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator). It's great a tool for managing monitoring configuration of your applications. VictoriaMetrics operator has api capability with it.
So you can use familiar CRD objects: `ServiceMonitor`, `PodMonitor`, `PrometheusRule`, `Probe` and `ScrapeConfig`. Or you can use VictoriaMetrics CRDs:

- `VMServiceScrape` - defines scraping metrics configuration from pods backed by services.
- `VMPodScrape` - defines scraping metrics configuration from pods.
- `VMRule` - defines alerting or recording rules.
- `VMProbe` - defines a probing configuration for targets with blackbox exporter.
- `VMScrapeConfig` - define a scrape config using any of the service discovery options supported in victoriametrics.

Besides, operator allows you to manage VictoriaMetrics applications inside kubernetes cluster and simplifies this process [quick-start](./docs/quick-start.md)
With CRD (Custom Resource Definition) you can define application configuration and apply it to your cluster [crd-objects](./docs/api.md).

 Operator simplifies VictoriaMetrics cluster installation, upgrading and managing.

 It has integration with VictoriaMetrics [vmbackupmanager](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/) - advanced tools for making backups. Check [Backup automation for VMSingle](./docs/resources/vmsingle.md#backup-automation) or [Backup automation for VMCluster](./docs/resources/vmcluster.md#backup-automation).

## Use cases

 For kubernetes-cluster administrators, it simplifies installation, configuration, management for `VictoriaMetrics` application. And the main feature of operator -  is ability to delegate applications monitoring configuration to the end-users.

 For applications developers, its great possibility for managing observability of applications. You can define metrics scraping and alerting configuration for your application and manage it with an application deployment process. Just define app_deployment.yaml, app_vmpodscrape.yaml and app_vmrule.yaml. That's it, you can apply it to a kubernetes cluster. Check [quick-start](./docs/quick-start.md) for an example.

## Operator vs helm-chart

VictoriaMetrics provides [helm charts](https://github.com/VictoriaMetrics/helm-charts). Operator makes the same, simplifies it and provides advanced features.

## Documentation

- quick start [doc](https://docs.victoriametrics.com/operator/quick-start/)
- high availability [doc](https://docs.victoriametrics.com/operator/resources/vmalert/#high-availability)
- relabeling configuration [doc](https://docs.victoriametrics.com/operator/resources/vmagent/#relabeling)
- managing crd objects versions [doc](https://docs.victoriametrics.com/operator/resources/#managing-versions-of-vm)
- design and description of implementation [design](https://docs.victoriametrics.com/operator/design/)
- operator objects description [doc](https://docs.victoriametrics.com/operator/api/)
- backups [docs](https://docs.victoriametrics.com/operator/resources/vmsingle/#backup-automation)
- external access to cluster resources[doc](https://docs.victoriametrics.com/operator/auth/)
- security [doc](https://docs.victoriametrics.com/operator/security/)
- resource validation [doc](https://docs.victoriametrics.com/operator/configuration/#crd-validation)

## Configuration

 Operator configured by env variables, list of it can be found at [link](https://docs.victoriametrics.com/operator/configuration/#environment-variables)

 It defines default configuration options, like images for components, timeouts, features.

## Kubernetes' compatibility versions

operator tested on officially supported Kubernetes versions

## Community and contributions

Feel free asking any questions regarding VictoriaMetrics:

- [slack](http://slack.victoriametrics.com/)
- [reddit](https://www.reddit.com/r/VictoriaMetrics/)
- [telegram-en](https://t.me/VictoriaMetrics_en)
- [telegram-ru](https://t.me/VictoriaMetrics_ru1)
- [google groups](https://groups.google.com/forum/#!forum/victorametrics-users)

## Development

Dependencies:

- kubebuilder v4
- golang 1.23+
- kubectl
- docker

start:

```bash
make run
```

### to run unit tests

```bash
make test
```

### to run e2e tests on automatically configured Kind cluster

```bash
# make test-e2e
```
