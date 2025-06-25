---
weight: 22
title: VMAnomaly
menu:
  docs:
    identifier: operator-cr-vmanomaly
    parent: operator-cr
    weight: 22
aliases:
  - /operator/resources/vmanomaly/
  - /operator/resources/vmanomaly/index.html
tags:
  - kubernetes
  - metrics
  - logs
  - anomaly
---
`VMAnomaly` - represents [VictoriaMetrics Anomaly Detection](https://docs.victoriametrics.com/anomaly-detection/) configuration {{% available_from "v0.60.0" "operator"%}}.

The `VMAnomaly` CRD declaratively defines a desired Anomaly Detection setup to run in a Kubernetes cluster.
It provides options to configure [sharding](https://docs.victoriametrics.com/anomaly-detection/scaling-vmanomaly/#horizontal-scalability), [replication](https://docs.victoriametrics.com/anomaly-detection/scaling-vmanomaly/#high-availability), persistent storage.

For each shard of `VMAnomaly` resource, the Operator deploys a properly configured `StatefulSet` in the same namespace.
Anomaly Detection pods can either use a configuration Secret created from the value specified in `spec.configRawYaml`, or use an existing secret specified in `spec.configSecret`.

## Configuration

The operator generates a configuration file for `VMAnomaly` based on user input at the definition of [CRD](https://docs.victoriametrics.com/operator/api/#vmanomaly).

Generated config stored at `Secret` created by the operator, it has the following name template `config-vmanomaly-CRD_NAME`.

This configuration file is mounted at `VMAnomaly` `Pod`.

**VMAnomaly is enterprise only, license is required for this CRD**. [Trial license can be requested](https://victoriametrics.com/products/enterprise/trial/) for `VMAnomaly` CRD evaluation.

In contrast to Anomaly Detection configuration, `VMAnomaly` CRD requires to define [`reader`](https://docs.victoriametrics.com/anomaly-detection/components/reader/), [`writer`](https://docs.victoriametrics.com/anomaly-detection/components/writer/) and [`monitoring`](https://docs.victoriametrics.com/anomaly-detection/components/monitoring/) sections in raw configuration (except `reader.queries`, which should be defined at `spec.configRawYaml` or `spec.configSecret`). `reader`, `writer` and `monitoring` have dedicated sections in `VMAnomaly` CRD:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  replicaCount: 2
  license:
    key: "xx"
  configRawYaml: |
    reader:
      queries:
        ingestion_rate:
          expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
          step: '1m'
    schedulers:
      scheduler_periodic_1m:
        class: "periodic"
        # class: "periodic" # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support)
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      model_univariate_1:
        class: 'zscore'
        z_threshold: 2.5
  reader:
    datasourceURL: http://vmsingle-read-example:8428
    samplingPeriod: 10s
  writer:
    datasourceURL: http://vmsingle-write-example:8428
  monitoring:
    push:
      url: http://vmsingle-monitoring-example:8428
```

### Reader, Writer and Monitoring

While Anomaly Detection [models](https://docs.victoriametrics.com/anomaly-detection/components/models/), [schedulers](https://docs.victoriametrics.com/anomaly-detection/components/schedulers/) and [settings](https://docs.victoriametrics.com/anomaly-detection/components/settings/) should be defined in `spec.configRawYaml` or `spec.configSecret`, [reader](https://docs.victoriametrics.com/anomaly-detection/components/reader/), [writer](https://docs.victoriametrics.com/anomaly-detection/components/writer/) and [monitoring](https://docs.victoriametrics.com/anomaly-detection/components/monitoring/) sections should defined at `spec.reader`, `spec.writer` and `spec.monitoring` respectively.

This was done to allow to use K8s secrets and configmaps as a source for TLS, basic and bearer secrets for reader, writer and monitoring endpoints. Also structure of this sections differ from Anomaly Detection configuration structure.

Given below reader, writer and monitoring configuration

```yaml
reader:
  class: vm
  datasource_url: http://localhost:8428
  sampling_period: 10s
  bearer_token: token
  queries:
    vmb:
      expr: avg(vm_blocks)
      step: 10s
      data_range: ['-inf', 'inf']
      tz: UTC
writer:
  class: vm
  datasource_url: http://localhost:8428/
  tenant_id: 0:0
  metric_format:
    __name__: vmanomaly_$VAR
    for: $QUERY_KEY
    run: test_metric_format
    config: io_vm_single.yaml
  import_json_path: /api/v1/import
  health_path: health
  user: foo
  password: bar
  tls_cert_file: cert-file
  tls_key_file: key-file
monitoring:
  pull:
    addr: 0.0.0.0
    port: 8080
  push:
    url: http://localhost:8480/
    tenant_id: 0:0
    user: USERNAME
    password: PASSWORD
    verify_tls: False
    timeout: 5s
    push_frequency: 15m
    extra_labels:
      job: vmanomaly-push
      test: test-1
```

maps into VMAnomaly CR

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  reader:
    datasourceURL: http://localhost:8428
    samplingPeriod: 10s
    bearer:
      bearerTokenSecret:
        name: k8s-secret-name
        key: token-secret-key
  writer:
    datasourceURL: http://localhost:8428/
    tenantID: 0:0
    metricFormat:
      name: vmanomaly_$VAR
      for: $QUERY_KEY
      extraLabels:
        run: test_metric_format
        config: io_vm_single.yaml
    importJsonPath: /api/v1/import
    healthPath: health
    basicAuth:
      username:
        name: k8s-basic-auth-secret-name
        key: username-secret-key
      password:
        name: k8s-basic-auth-secret-name
        key: password-secret-key
    tlsConfig:
      cert:
        name: k8s-tls-secret-name
        key: tls-cert-key
      keySecret:
        name: k8s-tls-secret-name
        key: tls-key
  monitoring:
    pull:
      addr: 0.0.0.0
      port: 8080
    push:
      url: http://localhost:8480/
      tenantID: 0:0
      username:
        name: k8s-basic-auth-secret-name
        key: username-secret-key
      password:
        name: k8s-basic-auth-secret-name
        key: password-secret-key
      tlsConfig:
        insecureSkipVerify: true
      timeout: 5s
      pushFrequency: 15m
      extraLabels:
        job: vmanomaly-push
        test: test-1
  configRawYaml: |
    reader:
      queries:
        vmb:
          expr: avg(vm_blocks)
          step: 10s
          data_range: ['-inf', 'inf']
          tz: UTC
```

### Using secret

Configuration can be defined in a manually created `Secret`, which must be created before `VMAnomaly` resource.

`Secret` selector must be defined at `VMAnomaly` `spec.configSecret` option:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: anomaly-config
  labels:
    app: vm-operator
type: Opaque
stringData:
  config.yaml: |
    reader:
      queries:
        ingestion_rate:
          expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
          step: '1m'
    schedulers:
      scheduler_periodic_1m:
        class: "periodic"
        # class: "periodic" # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support)
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      model_univariate_1:
        class: 'zscore'
        z_threshold: 2.5

---

apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  replicaCount: 2
  license:
    key: "xx"
  reader:
    datasourceURL: http://vmsingle-read-example:8428
    samplingPeriod: 10s
  writer:
    datasourceURL: http://vmsingle-write-example:8428
  configSecret:
    name: anomaly-config
    key: config.yaml
```

### Using inline raw config

Alternatively configuration can be defined as a `spec.configRawYaml` property value of `VMAnomaly` resource:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  replicaCount: 2
  reader:
    datasourceURL: http://vmsingle-read-example:8428
    samplingPeriod: 10s
  writer:
    datasourceURL: http://vmsingle-write-example:8428
  license:
    key: "xx"
  configRawYaml: |
    reader:
      queries:
        ingestion_rate:
          expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
          step: '1m'
    schedulers:
      scheduler_periodic_1m:
        class: "periodic"
        # class: "periodic" # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support)
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      model_univariate_1:
        class: 'zscore'
        z_threshold: 2.5
```

If both `configSecret` and `configRawYaml` are defined, only configuration from `configRawYaml` will be used. Values from `configSecret` will be ignored.

## High Availability and Sharding

Anomaly Detection can be deployed in [HA mode](https://docs.victoriametrics.com/anomaly-detection/scaling-vmanomaly/#high-availability) and scaled horizontally. Horizontal scaling is achieved by [automatically splitting the configuration into shards](https://new.docs.victoriametrics.com/anomaly-detection/scaling-vmanomaly/#sub-configuration) using the `spec.shardCount` parameter. The `spec.replicaCount` parameter defines the number of replicas per shard. The Operator creates one StatefulSet per shard (equal to `spec.shardCount`), with each StatefulSet containing `spec.replicaCount` replicas.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  replicaCount: 2
  shardCount: 2
  reader:
    datasourceURL: http://vmsingle-read-example:8428
    samplingPeriod: 10s
  writer:
    datasourceURL: http://vmsingle-write-example:8428
  license:
    key: "xx"
  configRawYaml: |
    reader:
      queries:
        ingestion_rate:
          expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
          step: '1m'
        ingestion_failed:
          expr: 'sum(rate(vm_rows_failed[5m])) by (type) > 0'
          step: '1m'
    schedulers:
      scheduler_periodic_1m:
        class: "periodic"
        # class: "periodic" # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support)
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      model_univariate_1:
        class: 'zscore'
        z_threshold: 2.5
```

## Version management

To set `VMAnomaly` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  image:
    tag: v1.24.0
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  image:
    tag: v1.24.0
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Resource management

You can specify resources for each `VMAnomaly` resource in the `spec` section of the `VMAnomaly` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: resources-example
spec:
  # ...
  resources:
    requests:
      memory: 64Mi
      cpu: 250m
    limits:
      memory: 128Mi
      cpu: 500m
  # ...
```

If these parameters are not specified, then,
by default all `VMAnomaly` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration):

- `VM_VMANOMALY_RESOURCE_LIMIT_MEM` - default memory limit for `VMAnomaly` pods,
- `VM_VMANOMALY_RESOURCE_LIMIT_CPU` - default memory limit for `VMAnomaly` pods,
- `VM_VMANOMALY_RESOURCE_REQUEST_MEM` - default memory limit for `VMAnomaly` pods,
- `VM_VMANOMALY_RESOURCE_REQUEST_CPU` - default memory limit for `VMAnomaly` pods.

These default parameters will be used if:

- `VM_VMANOMALY_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VMAnomaly` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VMAnomaly` spec have higher priority than operator parameters.

If you set `VM_VMANOMALY_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VMAnomaly` CRD,
then `VMAnomaly` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Examples

Below is an example of VMAnomaly setup with [periodic scheduler](https://docs.victoriametrics.com/anomaly-detection/components/scheduler/index.html#periodic-scheduler), [z-score model](https://docs.victoriametrics.com/anomaly-detection/components/models/#z-score), that is applied against data extracted using given `ingestion_rate` query from `http://vmsingle-read-example:8428` endpoint

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
spec:
  replicaCount: 1
  license:
    key: "xx"
  reader:
    datasourceURL: http://vmsingle-read-example:8428
    samplingPeriod: 10s
  writer:
    datasourceURL: http://vmsingle-write-example:8428
  configRawYaml: |-
    reader:
      queries:
        ingestion_rate:
          expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
          step: '1m'
    schedulers:
      scheduler_periodic_1m:
        class: "periodic"
        # class: "periodic" # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support)
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      model_univariate_1:
        class: 'zscore'
        z_threshold: 2.5
```
