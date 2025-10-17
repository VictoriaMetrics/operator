---
weight: 15
title: VMAnomalyScheduler
menu:
  docs:
    identifier: operator-cr-vmanomalyscheduler
    parent: operator-cr
    weight: 8
aliases:
  - /operator/resources/vmanomalyscheduler/
  - /operator/resources/vmanomalyscheduler/index.html
tags:
  - kubernetes
  - metrics
  - anomaly
---
The `VMAnomalyScheduler` CRD allows to declaratively define anomaly detection [schedulers](https://docs.victoriametrics.com/anomaly-detection/components/schedulers/).

`VMAnomalyScheduler` object updates [scheduler](https://docs.victoriametrics.com/anomaly-detection/components/schedulers/) section of [VMAnomaly](https://docs.victoriametrics.com/anomaly-detection/)
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
  schedulerSelector:
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
      test-example-periodic:
        class: "periodic"
        infer_every: "1m"
        fit_every: "2m"
        fit_window: "3h"
    models:
      test:
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

and `VMAnomalyScheduler` CR

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMAnomalyScheduler
metadata:
  name: example-periodic
  namespace: test
  labels:
    app: test
spec:
  class: periodic
  params:
    infer_every: 2m
    fit_every: 4m
    fit_window: 5h
```

result anomaly detection configuration is:

```yaml
schedulers:
  test-example-periodic:
    class: "periodic"
    infer_every: 2m
    fit_every: 4m
    fit_window: 5h
models:
  test:
    class: 'zscore'
    z_threshold: 2.5
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

You can see the full actual specification of the `VMAnomalyScheduler` resource in
the **[API docs -> VMAnomalyScheduler](https://docs.victoriametrics.com/operator/api/#vmanomalyscheduler)**.


