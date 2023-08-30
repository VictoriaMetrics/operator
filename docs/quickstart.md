---
sort: 1
weight: 1
title: QuickStart
---

# VictoriaMetrics Operator QuickStart

Operator serves to make running VictoriaMetrics applications on top of Kubernetes as easy as possible while preserving
Kubernetes-native configuration options.

## 1. Setup

You can find out how to and instructions for installing the VictoriaMetrics operator into your kubernetes cluster
on the [Setup page](https://docs.victoriametrics.com/operator/setup.html).

Here we will elaborate on just one of the ways - for instance, we will install operator via Helm-chart
[victoria-metrics-operator](https://github.com/VictoriaMetrics/helm-charts/blob/master/charts/victoria-metrics-operator/README.md):

Add repo with helm-chart:

```console
helm repo add vm https://victoriametrics.github.io/helm-charts/
helm repo update
```

Render `values.yaml` with default operator configuration:

```console
helm show values vm/victoria-metrics-operator > values.yaml
```

Now you can configure operator - open rendered `values.yaml` file in your text editor. For example:

```console
code values.yaml
```

<img src="quickstart_values.png" width="1000">

Now you can change configuration in `values.yaml`. For more details
see [configuration -> victoria-metrics-operator](https://docs.victoriametrics.com/operator/configuration.html#victoria-metrics-operator).

After finishing with `values.yaml`, you can test the installation with command:

```console
helm install vmoperator vm/victoria-metrics-operator -f values.yaml -n NAMESPACE --debug --dry-run
```

Where `NAMESPACE` is the namespace where you want to install operator.

If everything is ok, you can install operator with command:

```console
helm install vmoperator vm/victoria-metrics-operator -f values.yaml -n NAMESPACE
```

And check that operator is running:

```console
kubectl get pods -n NAMESPACE | grep 'operator'
```

## 2. Configure operator

TODO (+vars + monitoring + HA)

## 3. Create instances

TODO (+resources + high availability + auth + managing version)

## 4. Targets scraping

TODO (+migration from prometheus)

## 5. See the results

TODO

## 6. Backups

TODO

## 7. Anything else

TODO (guides, FAQ, resources, issues.)
