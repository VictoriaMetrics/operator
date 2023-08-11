# VMAlert

The `VMAlert` CRD declaratively defines a desired [VMAlert](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmalert)
setup to run in a Kubernetes cluster.

For each `VMAlert` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VMAlert `Pod`s are configured to mount a list of `Configmaps` prefixed with `<VMAlert-name>-number` containing
the configuration for alerting rules.

For each `VMAlert` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with
name `<VMAlert-name>`.

The CRD specifies which `VMRule`s should be covered by the deployed VMAlert instances based on label selection.
The Operator then generates a configuration based on the included `VMRule`s and updates the `Configmaps` containing
the configuration. It continuously does so for all changes that are made to `VMRule`s or to the `VMAlert` resource itself.

Alerting rules are filtered by selector `ruleNamespaceSelector` in `VMAlert` CRD definition. For selecting rules from all
namespaces you must specify it to empty value:

```yaml
spec:
  ruleNamespaceSelector: {}
```

## Specification

You can see the full actual specification of the `VMAlert` resource in the [API docs -> VMAlert](https://docs.victoriametrics.com/vmoperator/api.html#vmalert).

## High availability

**TODO**
