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

You can see the full actual specification of the `VMAlert` resource in the [API docs -> VMAlert](https://docs.victoriametrics.com/operator/api.html#vmalert).

## High availability

`VMAlert` can be launched with multiple replicas without an additional configuration as far [alertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html) is responsible for alert deduplication.

Note, if you want to use `VMAlert` with high-available [`VMAlertmanager`](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html), which has more than 1 replica. 
You have to specify all pod fqdns  at `VMAlert.spec.notifiers.[url]`. Or you can use service discovery for notifier, examples:

- alertmanager:
    ```yaml
    apiVersion: v1
    kind: Secret
    metadata:
      name: vmalertmanager-example-alertmanager
      labels:
        app: vm-operator
    type: Opaque
    stringData:
      alertmanager.yaml: |
        global:
          resolve_timeout: 5m
        route:
          group_by: ['job']
          group_wait: 30s
          group_interval: 5m
          repeat_interval: 12h
          receiver: 'webhook'
        receivers:
          - name: 'webhook'
            webhook_configs:
              - url: 'http://alertmanagerwh:30500/'
    
    ---
    
    apiVersion: operator.victoriametrics.com/v1beta1
    kind: VMAlertmanager
    metadata:
      name: example
      namespace: default
      labels:
       usage: dedicated
    spec:
      replicaCount: 2
      configSecret: vmalertmanager-example-alertmanager
      configSelector: {}
      configNamespaceSelector: {}
    ```
- vmalert with fqdns:
    ```yaml
    apiVersion: operator.victoriametrics.com/v1beta1
    kind: VMAlert
    metadata:
      name: example-ha
      namespace: default
    spec:
      datasource:
        url: http://vmsingle-example.default.svc:8429
      notifiers:
        - url: http://vmalertmanager-example-0.vmalertmanager-example.default.svc:9093
        - url: http://vmalertmanager-example-1.vmalertmanager-example.default.svc:9093
    ```
- vmalert with service discovery:
    ```yaml
    apiVersion: operator.victoriametrics.com/v1beta1
    kind: VMAlert
    metadata:
      name: example-ha
      namespace: default
    spec:
      datasource:
       url: http://vmsingle-example.default.svc:8429
      notifiers:
        - selector:
            namespaceSelector:
              matchNames: 
                - default
            labelSelector:
              matchLabels:
                  usage: dedicated
    ```
