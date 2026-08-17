---
weight: 21
title: VTAgent
menu:
  docs:
    identifier: operator-cr-vtagent
    parent: operator-cr
    weight: 21
aliases:
  - /operator/resources/vtagent/
  - /operator/resources/vtagent/index.html
tags:
  - kubernetes
  - traces
---
`VTAgent` allows accepting OTLP trace spans (HTTP and gRPC) and replicating them across one or more
[VictoriaTraces](https://docs.victoriametrics.com/victoriatraces/) instances,
using a persistent queue on disk in case a destination is temporarily unavailable for writing.

The `VTAgent` CRD declaratively defines a desired [vtagent](https://docs.victoriametrics.com/victoriatraces/vtagent/)
setup to run in a Kubernetes cluster.

`VTAgent` is deliberately minimal compared to [`VLAgent`](https://docs.victoriametrics.com/operator/resources/vlagent/): it accepts OTLP trace spans and sends data to VictoriaTraces.

## Basic configuration

To run VTAgent with a minimal configuration, you need to specify the `remoteWrite` addresses to replicate spans to:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: "VTAgent"
metadata:
  name: example
spec:
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
```

For each `VTAgent` resource, the Operator deploys a properly configured `StatefulSet` in the same namespace,
alongside a headless `Service` and a `VMPodScrape` for self-monitoring with name `vtagent-{metadata.name}`.

VTAgent allocates port 10429 by default for OTLP/HTTP ingestion. Below is an example of sending trace spans to the
[`/insert/opentelemetry/v1/traces`](https://docs.victoriametrics.com/victoriatraces/#sending-otlp-traces-to-victoriatraces) handler inside a cluster:

```sh
curl http://vtagent-example-0.vtagent-example.default.svc.cluster.local:10429/insert/opentelemetry/v1/traces \
    -H "Content-Type: application/json" \
    --data-binary '@spans.json'
```

## gRPC ingestion

In addition to OTLP/HTTP, `VTAgent` can accept trace spans over OTLP/gRPC. Set `spec.grpcSpec.listenPort` to enable
a gRPC listener alongside the default HTTP one:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: "VTAgent"
metadata:
  name: example
spec:
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
  grpcSpec:
    listenPort: 4317
```

TLS for the gRPC listener is disabled by default. To enable it, set `spec.grpcSpec.tlsConfig` with a certificate and
key, either referencing a `Secret` or a file already mounted into the pod:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: "VTAgent"
metadata:
  name: example
spec:
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
  grpcSpec:
    listenPort: 4317
    tlsConfig:
      certSecret:
        name: vtagent-grpc-tls
        key: tls.crt
      keySecret:
        name: vtagent-grpc-tls
        key: tls.key
```

## Replication and high availability

Every `remoteWrite` entry receives a full copy of every ingested span - `VTAgent` replicates (fans out); it does not
shard data across destinations. Listing multiple `remoteWrite` targets is therefore a way to replicate the same
trace spans to several independent VictoriaTraces instances or clusters, not to scale ingestion capacity.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: "VTAgent"
metadata:
  name: example
spec:
  remoteWrite:
    - url: "http://vtsingle-a:10428/insert/native"
    - url: "http://vtsingle-b:10428/insert/native"
```

## Specification

You can see the full actual specification of the `VTAgent` resource in the **[API docs -> VTAgent](https://docs.victoriametrics.com/operator/api/#vtagent)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

## Version management

To set `VTAgent` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaTraces/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTAgent
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/vtagent
    tag: v0.11.0
    pullPolicy: Always
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTAgent
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/vtagent
    tag: v0.11.0
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
```

## Resource management

You can specify resources for each `VTAgent` resource in the `spec` section of the `VTAgent` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTAgent
metadata:
  name: resources-example
spec:
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
  resources:
    requests:
      memory: "64Mi"
      cpu: "250m"
    limits:
      memory: "128Mi"
      cpu: "500m"
```

If these parameters are not specified, then, by default all `VTAgent` pods have resource requests and limits from
the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VTAGENTDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VTAgent` pods,
- `VM_VTAGENTDEFAULT_RESOURCE_LIMIT_CPU` - default CPU limit for `VTAgent` pods,
- `VM_VTAGENTDEFAULT_RESOURCE_REQUEST_MEM` - default memory request for `VTAgent` pods,
- `VM_VTAGENTDEFAULT_RESOURCE_REQUEST_CPU` - default CPU request for `VTAgent` pods.

These default parameters will be used if:

- `VM_VTAGENTDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VTAgent` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VTAgent` spec have higher priority than operator parameters.

If you set `VM_VTAGENTDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VTAgent` CRD,
then `VTAgent` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.
The reverse also works: specifying limits without requests skips the default request values too.

## Examples

### VTAgent with persistent buffering and multiple replicas

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VTAgent
metadata:
  name: example
spec:
  replicaCount: 2
  resources:
    requests:
      cpu: "50m"
      memory: "150Mi"
    limits:
      cpu: "500m"
      memory: "500Mi"
  persistentVolumeClaimRetentionPolicy:
    whenDeleted: Delete
  storage:
    volumeClaimTemplate:
      spec:
        resources:
          requests:
            storage: 10Gi
  remoteWrite:
    - url: "http://vtsingle-example:10428/insert/native"
      maxDiskUsage: 5GB
```
