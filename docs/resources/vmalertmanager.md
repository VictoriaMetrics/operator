# VMAlertmanager

The `VMAlertmanager` CRD declaratively defines a desired Alertmanager setup to run in a Kubernetes cluster.
It provides options to configure replication and persistent storage.

For each `Alertmanager` resource, the Operator deploys a properly configured `StatefulSet` in the same namespace.
The Alertmanager pods are configured to include a `Secret` called `<alertmanager-name>` which holds the used
configuration file in the key `alertmanager.yaml`.

When there are two or more configured replicas the Operator runs the Alertmanager instances in high availability mode.

## Specification

You can see the full actual specification of the `VMAlertmanager` resource in the [API docs -> VMAlert](https://docs.victoriametrics.com/operator/api.html#vmalertmanager).

## Configuration

The operator generates a configuration file for `VMAlertmanager` based on user input at the definition of `CRD`.

Generated config stored at `Secret` created by the operator, it has the following name template `vmalertmanager-CRD_NAME-config`.

This configuration file is mounted at `VMAlertmanager` `Pod`. A special side-car container tracks its changes and sends config-reload signals to `alertmanager` container.

### Using secret

Basically, you can use the global configuration defined at manually created `Secret`. This `Secret` must be created before `VMAlertmanager`.

Name of the `Secret` must be defined at `VMAlertmanager` `spec.configSecret` option:

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
      receiver: 'webhook'
    receivers:
      - name: 'webhook'
        webhook_configs:
          - url: 'http://alertmanagerwh:30500/'

---

apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: example-alertmanager
spec:
  replicaCount: 2
  configSecret: vmalertmanager-example-alertmanager
```

### Using inline raw config

Also, if there is no secret data at configuration, or you just want to redefine some global variables for `alertmanager`.
You can define configuration at `spec.configRawYaml` section of `VMAlertmanager` configuration:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: example-alertmanager
spec:
  replicaCount: 2
  configRawYaml: |
    global:
       resolve_timeout: 5m
    route:
      receiver: 'default'
      group_interval: 5m
      repeat_interval: 12h
    receivers:
    - name: 'default'
```

If both `configSecret` and `configRawYaml` are defined, only configuration from `configRawYaml` will be used. Values from `configRawYaml` will be ignored.

### Using VMAlertmanagerConfig

See details at [VMAlertmanagerConfig](https://docs.victoriametrics.com/operator/resources/vmalertmanagerconfig.html).

### Extra configuration files

`VMAlertmanager` specification has the following fields, that can be used to configure without editing raw configuration file:

- `spec.templates` - list of keys in `ConfigMaps`, that contains template files for `alertmanager`, e.g.:

  ```yaml
  apiVersion: operator.victoriametrics.com/v1beta1
  kind: VMAlertmanager
  metadata:
    name: example-alertmanager
  spec:
    replicaCount: 2
    templates:
      - Name: alertmanager-templates
        Key: my-template-1.tmpl
      - Name: alertmanager-templates
        Key: my-template-2.tmpl
  ---
  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: alertmanager-templates
  data:
      my-template-1.tmpl: |
          {{ define "hello" -}}
          hello, Victoria!
          {{- end }}
      my-template-2.tmpl: """
  ```

These templates will be automatically added to `VMAlertmanager` configuration and will be automatically reloaded on changes in source `ConfigMap`.
- `spec.configMaps` - list of `ConfigMap` names (in the same namespace) that will be mounted at `VMAlertmanager`
  workload and will be automatically reloaded on changes in source `ConfigMap`. Mount path is `/etc/vm/configs/<configmap-name>`.

### Behavior without provided config

If no configuration is provided, operator configures stub configuration with blackhole route.

## High Availability

The final step of the high availability scheme is Alertmanager, when an alert triggers, actually fire alerts against *all* instances of an Alertmanager cluster.

The Alertmanager, starting with the `v0.5.0` release, ships with a high availability mode. 
It implements a gossip protocol to synchronize instances of an Alertmanager cluster 
regarding notifications that have been sent out, to prevent duplicate notifications. 
It is an AP (available and partition tolerant) system. Being an AP system means that notifications are guaranteed to be sent at least once.

The Victoria Metrics Operator ensures that Alertmanager clusters are properly configured to run highly available on Kubernetes.

## Version management

To set `VMAlertmanager` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: example-vmalertmanager
spec:
  image:
    repository: prom/alertmanager
    tag: v0.25.0
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: example-vmalertmanager
spec:
  image:
    repository: prom/alertmanager
    tag: v0.25.0
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```
