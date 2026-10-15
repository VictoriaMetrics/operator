---
weight: 10
title: VMRule
menu:
  docs:
    identifier: operator-cr-vmrule
    parent: operator-cr
    weight: 10
aliases:
  - /operator/resources/vmrule/
  - /operator/resources/vmrule/index.html
tags:
  - kubernetes
  - metrics
  - logs
---
`VMRule` represents [alerting](https://docs.victoriametrics.com/victoriametrics/vmalert/#alerting-rules)
or [recording](https://docs.victoriametrics.com/victoriametrics/vmalert/#recording-rules) rules
for [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert/) instances.

The `VMRule` CRD declaratively defines groups of rules to be consumed by one or more `VMAlert` instances.
Rule expressions are written in [MetricsQL](https://docs.victoriametrics.com/victoriametrics/metricsql/) (inspired by PromQL),
[LogsQL](https://docs.victoriametrics.com/victorialogs/logsql/) or [Graphite](https://graphite.readthedocs.io/en/stable/render_api.html) query language,
depending on the [type of the rule group](https://docs.victoriametrics.com/operator/resources/vmrule/#rule-group-type).

`VMAlert` selects `VMRule` objects by labels and renders them into its rules configuration,
see [VMAlert rules](https://docs.victoriametrics.com/operator/resources/vmalert/#rules).

Alerts and recording rules can be saved and applied as YAML files, and dynamically loaded without requiring any restart.

See more details about rule configuration in [VMAlert docs](https://docs.victoriametrics.com/victoriametrics/vmalert/#quickstart).

## Specification

You can see the full actual specification of the `VMRule` resource in
the **[API docs -> VMRule](https://docs.victoriametrics.com/operator/api/#v1beta1-vmrule)**.

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vmrule/#examples) section.

## Rule group type

Each group in `spec.groups` has an optional `type` field, which defines the query language of the group's rule expressions
and the datasource API used by `vmalert` to evaluate them:

- `prometheus` (default) - [MetricsQL](https://docs.victoriametrics.com/victoriametrics/metricsql/) or [PromQL](https://prometheus.io/docs/prometheus/latest/querying/basics/) expressions,
  evaluated via the Prometheus querying API of [VictoriaMetrics](https://docs.victoriametrics.com/victoriametrics/vmalert/#victoriametrics)
  or any Prometheus-compatible datasource;
- `vlogs` - [LogsQL](https://docs.victoriametrics.com/victorialogs/logsql/) expressions,
  evaluated via the stats API of [VictoriaLogs](https://docs.victoriametrics.com/victorialogs/vmalert/);
- `graphite` - [Graphite](https://graphite.readthedocs.io/en/stable/render_api.html) expressions,
  evaluated via the [Graphite Render API](https://docs.victoriametrics.com/victoriametrics/vmalert/#graphite).

The operator validates `VMRule` objects with the vmalert's own parser: in the admission webhook if it is enabled,
or while building the `VMAlert` configuration otherwise.
The `-rule.defaultRuleType` flag set via `VMAlert` `spec.extraArgs` does not affect this validation,
so set `type` explicitly for every LogsQL and Graphite group, otherwise the `VMRule` is rejected
or skipped with an expression parsing error.

A `VMAlert` has a single `datasource`, so rules of different types are usually evaluated by separate `VMAlert` instances,
each pointed to its datasource and selecting its `VMRule` objects via `ruleSelector`.
It is also possible to route requests of a single `VMAlert` to several datasources via `vmauth`, see
[how to use one vmalert for VictoriaLogs and VictoriaMetrics rules at the same time](https://docs.victoriametrics.com/victorialogs/vmalert/#how-to-use-one-vmalert-for-victorialogs-and-victoriametrics-rules-at-the-same-time).

See [group attributes](https://docs.victoriametrics.com/victoriametrics/vmalert/#groups) in the vmalert docs for additional info.

## Enterprise features

Custom resource `VMRule` supports feature [Multitenancy](https://docs.victoriametrics.com/victoriametrics/vmalert/#multitenancy)
from [VictoriaMetrics Enterprise](https://docs.victoriametrics.com/victoriametrics/enterprise/#victoriametrics-enterprise-features).

### Multitenancy

For using [Multitenancy](https://docs.victoriametrics.com/victoriametrics/vmalert/#multitenancy) in `VMRule`
you need to **[enable VMAlert Enterprise](https://docs.victoriametrics.com/operator/resources/vmalert/#enterprise-features)**.

After that you can add `tenant` field for groups in `VMRule`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: ent-example
spec:
  groups:
    - name: vmalert-1
    # using enterprise features: multitenancy,
    # read more in https://docs.victoriametrics.com/victoriametrics/vmalert/#multitenancy
      tenant: 1
      rules:
        - alert: vmalert config reload error
          expr: delta(vmalert_config_last_reload_errors_total[5m]) > 0
          for: 10s
          labels:
            severity: major
            job:  "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'error reloading vmalert config, reload count for 5 min {{ $value }}'
```

## Examples

### Alerting rule

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: alerting-example
spec:
  groups:
    - name: vmalert
      rules:
        - alert: vmalert config reload error
          expr: delta(vmalert_config_last_reload_errors_total[5m]) > 0
          for: 10s
          labels:
            severity: major
            job:  "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'error reloading vmalert config, reload count for 5 min {{ $value }}'
```

### Recording rule

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: recording-example
spec:
  groups:
    - name: vmrule_recording_groupname
      interval: 1m
      rules:
        - record: vm_http_request_errors_total:sum_by_cluster_namespace_job:rate:5m
          expr: |-
            sum by (cluster, namespace, job) (
              rate(vm_http_request_errors_total[5m])
            )
```

### Alerting rule for VictoriaLogs

The group has `type: vlogs` and its expression is written in [LogsQL](https://docs.victoriametrics.com/victorialogs/logsql/).

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: logs-alerting-example
spec:
  groups:
    - name: service-logs
      type: vlogs
      interval: 5m
      rules:
        - alert: TooManyErrorLogs
          expr: '{env=prod} status:in(error,warn) | stats by (k8s.pod.name) count() as error_logs | filter error_logs:>10'
          annotations:
            description: 'Too many errors and warnings on pod {{ index .Labels "k8s.pod.name" }} during the last 5 minutes: {{ $value }}'
```

See [alerting with logs](https://docs.victoriametrics.com/victorialogs/vmalert/) for details.

### Recording rule for Graphite

The group has `type: graphite` and its expression is written in [Graphite](https://graphite.readthedocs.io/en/stable/render_api.html) query language,
see [Graphite integration](https://docs.victoriametrics.com/victoriametrics/vmalert/#graphite) in the vmalert docs.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: graphite-recording-example
spec:
  groups:
    - name: graphite
      type: graphite
      interval: 1m
      rules:
        - record: app_requests_total
          expr: sumSeries(app.*.requests)
```
