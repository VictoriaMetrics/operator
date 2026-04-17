---
weight: 15
title: VMAnomalyConfig
menu:
  docs:
    identifier: operator-cr-vmanomalyconfig
    parent: operator-cr
    weight: 8
aliases:
  - /operator/resources/vmanomalyconfig/
  - /operator/resources/vmanomalyconfig/index.html
tags:
  - kubernetes
  - metrics
  - anomaly
---
The `VMAnomalyConfig` CRD allows declaratively defining anomaly detection [models](https://docs.victoriametrics.com/anomaly-detection/components/models/), [schedulers](https://docs.victoriametrics.com/anomaly-detection/components/schedulers/), and [queries](https://docs.victoriametrics.com/anomaly-detection/components/reader/#per-query-parameters).

`VMAnomalyConfig` object updates `models`, `schedulers` and `reader.queries` sections of [VMAnomaly](https://docs.victoriametrics.com/anomaly-detection/)
configuration by adding items with `{metadata.namespace}-{metadata.name}` key prefix. If at least one generated item collides with an existing key, only the colliding item is skipped, while other valid items from the same `VMAnomalyConfig` are still added to the resulting configuration. Check the `VMAnomalyConfig` status and related events to identify skipped items caused by collisions.

With given `VMAnomaly` CR:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomaly
metadata:
  name: example
  namespace: test
spec:
  replicaCount: 2
  license:
    key: "xx"
  configSelector:
    matchExpressions:
    - key: app
      operator: In
      values: [test]
  configRawYaml: |
    reader:
      queries:
        ingestion_rate:
          expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
          step: '1m'
    schedulers:
      scheduler_periodic_1m:
        class: "periodic"
        # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      zscore:
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

and `VMAnomalyConfig` CR

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomalyConfig
metadata:
  name: example
  namespace: test
  labels:
    app: test
spec:
  models:
    zscore:
      class: zscore
      z_threshold: 3.0
  schedulers:
    periodic:
      class: "periodic"
      # or class: "scheduler.periodic.PeriodicScheduler" until v1.13.0 with class alias support
      infer_every: "1m"
      fit_every: "2m"
      fit_window: "3h"
  queries:
    delete-rate:
      expr: 'sum(rate(vm_rows_deleted_total[5m])) by (type) > 0'
      step: '1m'
```

The result anomaly detection configuration is:

```yaml
schedulers:
  test-example-periodic:
    class: "periodic"
    infer_every: "1m"
    fit_every: "2m"
    fit_window: "3h"
  scheduler_periodic_1m:
    class: "periodic"
    infer_every: "1m"
    fit_every: "2m"
    fit_window: "3h"
models:
  test-example-zscore:
    class: 'zscore'
    z_threshold: 3.0
reader:
  datasourceURL: http://vmsingle-read-example:8428
  samplingPeriod: 10s
  queries:
    ingestion_rate:
      expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
      step: '1m'
    test-example-delete-rate:
      expr: 'sum(rate(vm_rows_deleted_total[5m])) by (type) > 0'
      step: '1m'
writer:
  datasourceURL: http://vmsingle-write-example:8428
```

## Specification

You can see the full actual specification of the `VMAnomalyConfig` resource in
the **[API docs -> VMAnomalyConfig](https://docs.victoriametrics.com/operator/api/#vmanomalyconfig)**.
