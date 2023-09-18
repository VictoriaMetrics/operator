# VMAlert

The `VMAlert` CRD declaratively defines a desired [VMAlert](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmalert)
setup to run in a Kubernetes cluster.

For each `VMAlert` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VMAlert `Pod`s are configured to mount a list of `Configmaps` prefixed with `<VMAlert-name>-number` containing
the configuration for alerting rules.

For each `VMAlert` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with
name `<VMAlert-name>`.

## Specification

You can see the full actual specification of the `VMAlert` resource in the [API docs -> VMAlert](https://docs.victoriametrics.com/operator/api.html#vmalert).

## Rules

The CRD specifies which `VMRule`s should be covered by the deployed `VMAlert` instances based on label selection.
The Operator then generates a configuration based on the included `VMRule`s and updates the `Configmaps` containing
the configuration. It continuously does so for all changes that are made to `VMRule`s or to the `VMAlert` resource itself.

Alerting rules are filtered by selectors `ruleNamespaceSelector` and `ruleSelector` in `VMAlert` CRD definition. 
For selecting rules from all namespaces you must specify it to empty value:

```yaml
spec:
  ruleNamespaceSelector: {}
```

[VMRUle](https://docs.victoriametrics.com/operator/resources/vmrule.html) objects are generates part of [VMAlert](https://docs.victoriametrics.com/operator/resources/vmalert.html) configuration.

For filtering rules `VMAlert` uses selectors `ruleNamespaceSelector` and `ruleSelector`.
It allows configuring rules access control across namespaces and different environments.
Specification of selectors you can see in [this doc](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#labelselector-v1-meta).

In addition to the above selectors, the filtering of objects in a cluster is affected by the field `selectAllByDefault` of `VMAlert` spec and environment variable `WATCH_NAMESPACE` for operator.

Following rules are applied:

| `ruleNamespaceSelector` | `ruleSelector` | `selectAllByDefault` | `WATCH_NAMESPACE` | Selected rules                                                                                     |
|-------------------------|----------------|----------------------|-------------------|----------------------------------------------------------------------------------------------------|
| undefined               | undefined      | false                | undefined         | nothing                                                                                            |
| undefined               | undefined      | **true**             | undefined         | all rules in the cluster                                                                           |
| **defined**             | undefined      | any                  | undefined         | all rules are matching at namespaces for given `ruleNamespaceSelector`                             |
| undefined               | **defined**    | any                  | undefined         | all rules only at `VMAlert`'s namespace are matching for given `ruleSelector`                      |
| **defined**             | **defined**    | any                  | undefined         | all rules only at namespaces matched `ruleNamespaceSelector` for given `ruleSelector` are matching |
| any                     | undefined      | any                  | **defined**       | all rules only at `VMAlert`'s namespace                                                            |
| any                     | **defined**    | any                  | **defined**       | all rules only at `VMAlert`'s namespace for given `ruleSelector` are matching                      |

More details about `WATCH_NAMESPACE` variable you can read in [this doc](https://docs.victoriametrics.com/operator/configuration.html#namespaced-mode).

Here are some examples of `VMAlert` configuration with selectors:

```yaml
# select all scrape objects in the cluster
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: vmalert-select-all
spec:
  # ...
  selectAllByDefault: true

---

# select all scrape objects in specific namespace (my-namespace)
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: vmalert-select-ns
spec:
  # ...
  ruleNamespaceSelector: 
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
```

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
      # ...
    
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
      # ...
    ```
- vmalert with fqdns:
    ```yaml
    apiVersion: operator.victoriametrics.com/v1beta1
    kind: VMAlert
    metadata:
      name: example-ha
      namespace: default
    spec:
      replicaCount: 2
      datasource:
        url: http://vmsingle-example.default.svc:8429
      notifiers:
        - url: http://vmalertmanager-example-0.vmalertmanager-example.default.svc:9093
        - url: http://vmalertmanager-example-1.vmalertmanager-example.default.svc:9093
      evaluationInterval: "10s"
      ruleSelector: {}
      # ...
    ```
- vmalert with service discovery:
    ```yaml
    apiVersion: operator.victoriametrics.com/v1beta1
    kind: VMAlert
    metadata:
      name: example-ha
      namespace: default
    spec:
      replicaCount: 2
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
      evaluationInterval: "10s"
      ruleSelector: {}
      # ...
    ```

## Version management

To set `VMAlert` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  image:
    repository: victoriametrics/victoria-metrics
    tag: v1.93.4
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  image:
    repository: victoriametrics/victoria-metrics
    tag: v1.93.4
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```
