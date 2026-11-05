---
weight: 2
title: VMAlert
menu:
  docs:
    identifier: operator-cr-vmalert
    parent: operator-cr
    weight: 2
aliases:
  - /operator/resources/vmalert/
  - /operator/resources/vmalert/index.html
tags:
  - kubernetes
  - metrics
  - logs
---
`VMAlert` - executes a list of given [alerting](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
or [recording](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/) rules against the configured datasource.

The `VMAlert` CRD declaratively defines a desired [VMAlert](https://docs.victoriametrics.com/victoriametrics/vmalert/)
setup to run in a Kubernetes cluster.

Only `datasource` is required. Alerting rules also need a notifier, configured with `notifier`, `notifiers`,
`notifierConfigRef`, or the `notifier.url`, `notifier.config` or `notifier.blackhole` flags in `extraArgs`.
If no notifier is configured:

- alerting rules from `VMRule`s are skipped, and only their recording rules are loaded;
- `VMAlert` with `rulePath` fails to reconcile, since the operator can't check whether these files contain alerting rules.

For other config parameters check [doc](https://docs.victoriametrics.com/operator/api/#v1beta1-vmalert).

For each `VMAlert` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VMAlert `Pod`s are configured to mount a list of `ConfigMap`s named `vm-<VMAlert-name>-rulefiles-<number>`
containing the alerting and recording rules.

For each `VMAlert` resource, the Operator adds a `Service` in the same namespace named
`vmalert-<VMAlert-name>`, or `<VMAlert-name>` if `useLegacyNaming` is set. Unless `disableSelfServiceScrape` is set, it also adds a `VMServiceScrape` with the same name.

## Specification

You can see the full actual specification of the `VMAlert` resource in the **[API docs -> VMAlert](https://docs.victoriametrics.com/operator/api/#v1beta1-vmalert)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vmalert/#examples) section.

## Rules

`VMAlert` loads alerting and recording rules from [VMRule](https://docs.victoriametrics.com/operator/resources/vmrule/) objects.
The Operator renders the selected `VMRule`s into the rule `ConfigMap`s and updates them whenever a `VMRule`
or the `VMAlert` itself changes.

`VMRule`s are selected by two [label selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors)
in the `VMAlert` spec:

- `ruleNamespaceSelector` selects the namespaces to look for `VMRule`s in;
- `ruleSelector` selects `VMRule`s by their labels.

They control which rules each `VMAlert` loads, for example per team or environment. The result also depends on
`spec.selectAllByDefault` and on the operator's `WATCH_NAMESPACE` environment variable:

| `ruleNamespaceSelector` | `ruleSelector` | `selectAllByDefault` | `WATCH_NAMESPACE` | Selected VMRules                                                                     |
|-------------------------|----------------|----------------------|-------------------|--------------------------------------------------------------------------------------|
| undefined               | undefined      | false                | *any*             | nothing                                                                              |
| undefined               | undefined      | **true**             | undefined         | all VMRules in the cluster                                                           |
| **defined**             | undefined      | *any*                | undefined         | all VMRules in the namespaces matched by `ruleNamespaceSelector`                     |
| undefined               | **defined**    | *any*                | undefined         | VMRules matching `ruleSelector` in the `VMAlert` namespace                           |
| **defined**             | **defined**    | *any*                | undefined         | VMRules matching `ruleSelector` in the namespaces matched by `ruleNamespaceSelector` |
| undefined               | undefined      | **true**             | **defined**       | all VMRules in the watched namespaces                                                |
| **defined**             | undefined      | *any*                | **defined**       | all VMRules in the watched namespaces                                                |
| *any*                   | **defined**    | *any*                | **defined**       | VMRules matching `ruleSelector` in the watched namespaces                            |

Notes:

- `selectAllByDefault` has an effect only when both selectors are undefined.
- An empty selector (`{}`) matches everything, e.g. `ruleNamespaceSelector: {}` matches all namespaces.
  A `ruleNamespaceSelector` that matches no namespaces selects nothing.
- When `WATCH_NAMESPACE` is set, `ruleNamespaceSelector` is ignored and `VMRule`s are selected from all watched
  namespaces, even when only `ruleSelector` is defined. See [namespaced mode](https://docs.victoriametrics.com/operator/configuration/#namespaced-mode).

Examples:

```yaml
# select all VMRules in the cluster
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: select-all
spec:
  # ...
  selectAllByDefault: true

---

# select all VMRules in my-namespace
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: select-ns
spec:
  # ...
  ruleNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace

---

# select VMRules labeled team: payments in all namespaces
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: select-team
spec:
  # ...
  ruleNamespaceSelector: {}
  ruleSelector:
    matchLabels:
      team: payments
```

### Rule files

Besides `VMRule`s, `VMAlert` can load rules from files listed in `spec.rulePath`. Each path is passed to `vmalert`
as an additional `-rule` command-line argument, next to the rules from `VMRule`s. For example, rules from a `ConfigMap`
mounted with `spec.configMaps`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example
spec:
  # ...
  configMaps:
    - extra-rules
  rulePath:
    - /etc/vm/configs/extra-rules/*.yaml
```

With [VictoriaMetrics Enterprise](https://docs.victoriametrics.com/operator/resources/vmalert/#reading-rules-from-object-storage),
`rulePath` can also point to `s3://` or `gs://` paths.

Don't set `rule` in [extraArgs](https://docs.victoriametrics.com/operator/resources/#extra-arguments): it replaces all `-rule`
arguments set by the operator, so rules from `VMRule`s and `spec.rulePath` are no longer loaded.

## High availability

`VMAlert` can be launched with multiple replicas without an additional configuration as long as [alertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager/) is responsible for alert deduplication.

If [`VMAlertmanager`](https://docs.victoriametrics.com/operator/resources/vmalertmanager/) has more than one replica,
list every pod's FQDN in `VMAlert.spec.notifiers[].url`, or use notifier service discovery. Examples:

- alertmanager:
    ```yaml
    apiVersion: v1
    kind: Secret
    metadata:
      name: vmalertmanager-example
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
      configSecret: vmalertmanager-example
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
        url: http://vmsingle-example.default.svc:8428
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
        url: http://vmsingle-example.default.svc:8428
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

In addition, you need to specify `remoteWrite` and `remoteRead` urls for restoring alert states after restarts:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-ha
  namespace: default
spec:
  replicaCount: 2
  evaluationInterval: "10s"
  selectAllByDefault: true
  datasource:
    url: http://vmselect-demo.vm.svc:8481/select/0/prometheus
  notifiers:
    - url: http://vmalertmanager-example-0.vmalertmanager-example.default.svc:9093
    - url: http://vmalertmanager-example-1.vmalertmanager-example.default.svc:9093
  remoteWrite:
    url: http://vminsert-demo.vm.svc:8480/insert/0/prometheus
  remoteRead:
    url: http://vmselect-demo.vm.svc:8481/select/0/prometheus
```

More details about `remoteWrite` and `remoteRead` you can read in [vmalert docs](https://docs.victoriametrics.com/victoriametrics/vmalert/#alerts-state-on-restarts).

## Version management

To set `VMAlert` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/vmalert
    tag: v1.110.13
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/vmalert
    tag: v1.110.13
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Resource management

You can specify resources for each `VMAlert` resource in the `spec` section of the `VMAlert` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: resources-example
spec:
  # ...
  resources:
    requests:
      memory: "64Mi"
      cpu: "250m"
    limits:
      memory: "128Mi"
      cpu: "500m"
  # ...
```

If these parameters are not specified, then,
by default all `VMAlert` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VMALERTDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VMAlert` pods, `500Mi`,
- `VM_VMALERTDEFAULT_RESOURCE_LIMIT_CPU` - default CPU limit for `VMAlert` pods, `200m`,
- `VM_VMALERTDEFAULT_RESOURCE_REQUEST_MEM` - default memory request for `VMAlert` pods, `200Mi`,
- `VM_VMALERTDEFAULT_RESOURCE_REQUEST_CPU` - default CPU request for `VMAlert` pods, `50m`.

These default parameters will be used if:

- `VM_VMALERTDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VMAlert` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VMAlert` spec has higher priority than operator parameters.

If you set `VM_VMALERTDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VMAlert` CRD,
then `VMAlert` pods will be created without resource requests and limits.

Defaults are applied per resource: setting a request or a limit for CPU disables both default CPU values,
and the same goes for memory. For example, you can specify requests without limits, and default limits will not be added.

## Enterprise features

VMAlert supports features [Reading rules from object storage](https://docs.victoriametrics.com/victoriametrics/vmalert/#reading-rules-from-object-storage)
and [Multitenancy](https://docs.victoriametrics.com/victoriametrics/vmalert/#multitenancy)
from [VictoriaMetrics Enterprise](https://docs.victoriametrics.com/victoriametrics/enterprise/#victoriametrics-enterprise-features).

For using Enterprise version of [vmalert](https://docs.victoriametrics.com/victoriametrics/vmalert/) you need to:

- specify license at either [`spec.license.key`](https://docs.victoriametrics.com/operator/api/#v1beta1-license-key) or [`spec.license.keyRef`](https://docs.victoriametrics.com/operator/api/#v1beta1-license-keyref);
- change version of `vmalert` to version with `-enterprise` suffix using [Version management](https://docs.victoriametrics.com/operator/resources/vmalert/#version-management).

### Reading rules from object storage

After that you can add `s3://` or `gs://` paths to [`spec.rulePath`](https://docs.victoriametrics.com/operator/resources/vmalert/#rule-files).

More details about reading rules from object storage you can read in [vmalert docs](https://docs.victoriametrics.com/victoriametrics/vmalert/#reading-rules-from-object-storage).

Here is a complete example for [Reading rules from object storage](https://docs.victoriametrics.com/victoriametrics/vmalert/#reading-rules-from-object-storage):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: ent-example
spec:
  # enabling enterprise features
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  image:
    tag: v1.110.13-enterprise
  # using enterprise features: Reading rules from object storage
  # more details about reading rules from object storage you can read on https://docs.victoriametrics.com/victoriametrics/vmalert/#reading-rules-from-object-storage
  rulePath:
    - s3://bucket/dir/alert.rules
  # ...other fields...
```

### Multitenancy

After enabling enterprise version you can use [Multitenancy](https://docs.victoriametrics.com/victoriametrics/vmalert/#multitenancy)
feature in `VMAlert`.

For that you need to set `clusterMode` command-line flag
with [extraArgs](https://docs.victoriametrics.com/operator/resources/#extra-arguments)
and specify `tenant` field for groups
in [VMRule](https://docs.victoriametrics.com/operator/resources/vmrule/#enterprise-features):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: ent-example
spec:
  # enabling enterprise features
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  image:
    tag: v1.110.13-enterprise
  extraArgs:
    # using enterprise features: multitenancy,
    # read more in https://docs.victoriametrics.com/victoriametrics/vmalert/#multitenancy
    clusterMode: "true"

  # ...other fields...

---

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

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example
spec:
  replicaCount: 1
  datasource:
    url: "http://vmsingle-example-persisted.default.svc:8428"
  notifier:
    url: "http://vmalertmanager-example.default.svc:9093"
  evaluationInterval: "30s"
  selectAllByDefault: true
```
