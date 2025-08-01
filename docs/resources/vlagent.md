---
weight: 10
title: VLAgent
menu:
  docs:
    identifier: operator-cr-vlagent
    parent: operator-cr
    weight: 1
aliases:
  - /operator/resources/vlagent/
  - /operator/resources/vlagent/index.html
tags:
  - kubernetes
  - metrics
  - logs
---
`VLAgent` {{% available_from "v0.61.0" "operator" %}} allows collecting logs from multiple sources
and replicating them across different VictoriaLogs clusters
using a persistent queue in case a cluster is temporarily unavailable for writing.

The `VLAgent` CRD declaratively defines a desired [VLAgent](https://docs.victoriametrics.com/victorialogs/vlagent/)
setup to run in a Kubernetes cluster.

## Basic configuration

To run VLAgent with a minimal configuration, you need to specify the `remoteWrite` addresses for data replication between them:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: "VLAgent"
metadata:
  name: example
spec:
  remoteWrite:
    - url: "http://VictoriaLogs-1:9428/internal/insert"
    - url: "http://VictoriaLogs-2:9428/internal/insert"
```

This will allow you to start VLAgent so it can receive logs from [all supported sources](https://docs.victoriametrics.com/victorialogs/data-ingestion/),
similar to how vlinsert works, with the difference that vlagent replicates data and,
in case one of the `remoteWrite` endpoints is unavailable, stores the data on disk for later delivery.
This makes it resilient to cluster unavailability.

VLAgent allocates port 9429, so you can use this port to send requests.
Below is an example of sending a log to the [`/jsonline`](https://docs.victoriametrics.com/victorialogs/data-ingestion/#json-stream-api)
handler inside a cluster:

```sh
curl http://vlagent-example.default.svc.cluster.local:9429/insert/jsonline \
    -H "Content-Type: application/json" \
    --data-binary '{"_msg":"foobar"}'
```

As a `remoteWrite` target, VLAgent supports all VictoriaLogs components:
[VictoriaLogs Single-node](https://docs.victoriametrics.com/victorialogs/#high-availability-ha-setup-with-victorialogs-single-node-instances),
[vlinsert](https://docs.victoriametrics.com/victorialogs/cluster/), and
[vlagent](https://docs.victoriametrics.com/victorialogs/vlagent/).

## Specification

You can see the full actual specification of the `VLAgent` resource in the **[API docs -> VLAgent](https://docs.victoriametrics.com/operator/api/#vlagent)**.

If you can't find necessary field in the specification of the custom resource,
see [extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](#Examples) section.

## Version management

To set `VLAgent` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaLogs/releases):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/victoria-logs
    tag: v1.26.0
    pullPolicy: Always
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/victoria-logs
    tag: v1.26.0
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
```

## Resource management

You can specify resources for each `VLAgent` resource in the `spec` section of the `VLAgent` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: resources-example
spec:
    resources:
      requests:
        memory: "64Mi"
        cpu: "250m"
      limits:
        memory: "128Mi"
        cpu: "500m"
```

If these parameters are not specified, then,
by default all `VLAgent` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration):

- `VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VLAgent` pods,
- `VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VLAgent` pods,
- `VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VLAgent` pods,
- `VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VLAgent` pods.

These default parameters will be used if:

- `VM_VLOGSDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VLAgent` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VLAgent` spec have higher priority than operator parameters.

If you set `VM_VLOGSDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VLAgent` CRD,
then `VLAgent` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Storage management

In case of errors sending logs to `remoteWrite`,
VLAgent begins persistently queuing logs for sending, using disk storage for this purpose.
By default, VLAgent uses an [ephemeral storage](https://kubernetes.io/docs/concepts/storage/ephemeral-volumes/),
which can lead to log loss if the pod is deleted for any reason at a time when `remoteWrite` was unavailable to receive logs.

To configure a volume that is resilient to pod deletion, set the storage section as shown below:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: resources-example
spec:
  storage:
    volumeClaimTemplate:
      spec:
        resources:
          requests:
            storage: 20Gi
```

This will create a `PersistentVolumeClaim` with the specified size.

You can also define your own volume and provide its mount path, as shown below:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: resources-example
spec:
  remoteWriteSettings:
    tmpDataPath: "/vlagent-remotewrite-data"
  volumes:
    - name: remotewrite-data
      hostPath:
        path: /vlagent-remotewrite-data
  volumeMounts:
    - mountPath: /vlagent-remotewrite-data
      name: remotewrite-data
```

Optionally, you can specify the `maxDiskUsagePerURL` parameter,
which allows you to set the maximum disk space usage for each configured `remoteWrite`.
This setting is useful to prevent exceeding the allocated disk size, for example, if you are using `hostPath`:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: resources-example
spec:
  remoteWriteSettings:
    maxDiskUsagePerURL: "1Gi"
```

## Examples

### Set max log size

You can set a flag using the `extraArgs` section.
For example, if you want to override the maximum log size:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: example
spec:
  extraArgs:
    insert.maxLineSizeBytes: 1MiB
  remoteWrite:
    - url: "http://VictoriaLogs:9428/internal/insert"
```

To see all vlagent flags follow [this documentation](https://docs.victoriametrics.com/victorialogs/vlagent/#advanced-usage).

### Setup TLS

Specify the `caFile` path to enable the collector to validate the server’s TLS certificate.
This is necessary when the endpoint uses a certificate issued by a custom or self-signed Certificate Authority (CA):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VLAgent
metadata:
  name: example
spec:
  remoteWrite:
    - url: "https://VictoriaLogs:9428/internal/insert"
      tlsConfig:
        caFile: /etc/tls/ca.crt
  volumes:
    - name: cert-ca
      secret:
        secretName: cert-ca
  volumeMounts:
    - name: cert-ca
      mountPath: /etc/tls
      readOnly: true
```
