# VMRule

`VMRule` represents [alerting](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
or [recording](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/) rules 
for [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html) instances.

The `VMRule` CRD declaratively defines a desired Prometheus rule to be consumed by one or more VMAlert instances.

`VMRule` object generates [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html) 
with ruleset defined at `VMRule` spec.

Alerts and recording rules can be saved and applied as YAML files, and dynamically loaded without requiring any restart.

See more details about rule configuration in [VMAlert docs](https://docs.victoriametrics.com/vmalert.html#quickstart).

## Specification

You can see the full actual specification of the `VMRule` resource in
the **[API docs -> VMRule](https://docs.victoriametrics.com/operator/api.html#vmrule)**.

Also, you can check out the [examples](#examples) section.

## Examples

### Alerting rule

```yaml
kind: VMRule
metadata:
  name: vmrule-alerting-example
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
kind: VMRule
metadata:
  name: vmrule-recording-example
spec:
  groups:
    - name: vmalert
      interval: 1m
      rules:
        - alert: vmalert config reload error
          expr: |-
            sum by (cluster, namespace, job) (
              rate(vm_http_request_errors_total[5m])
            )
```
