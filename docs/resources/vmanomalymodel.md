---
weight: 15
title: VMAnomalyModel
menu:
  docs:
    identifier: operator-cr-vmanomalymodel
    parent: operator-cr
    weight: 8
aliases:
  - /operator/resources/vmanomalymodel/
  - /operator/resources/vmanomalymodel/index.html
tags:
  - kubernetes
  - metrics
  - anomaly
---
The `VMAnomalyModel` CRD allows to declaratively define anomaly detection [models](https://docs.victoriametrics.com/anomaly-detection/components/models/).

`VMAnomalyModel` object updates [model](https://docs.victoriametrics.com/anomaly-detection/components/models/) section of [VMAnomaly](https://docs.victoriametrics.com/anomaly-detection/)
configuration by adding item with `{metadata.namespace}-{metadata.name}` key and `spec.params` value. If item with given key exists operator will override it:

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
  modelSelector:
    objectSelector:
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
      test-example-univariate:
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

and `VMAnomalyModel` CR

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomalyModel
metadata:
  name: example-univariate
  namespace: test
  labels:
    app: test
spec:
  class: zscore
  params:
    z_threshold: 3.0
```

result anomaly detection configuration is:

```yaml
schedulers:
  scheduler_periodic_1m:
    class: "periodic"
    infer_every: "1m"
    fit_every: "2m"
    fit_window: "3h"
models:
  test-example-univariate:
    class: 'zscore'
    z_threshold: 3.0
reader:
  datasourceURL: http://vmsingle-read-example:8428
  samplingPeriod: 10s
  queries:
    ingestion_rate:
      expr: 'sum(rate(vm_rows_inserted_total[5m])) by (type) > 0'
      step: '1m'
writer:
  datasourceURL: http://vmsingle-write-example:8428
```

## Specification

You can see the full actual specification of the `VMAnomalyModel` resource in
the **[API docs -> VMAnomalyModel](https://docs.victoriametrics.com/operator/api/#vmanomalymodel)**.


