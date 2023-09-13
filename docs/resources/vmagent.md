# VMAgent

The `VMAgent` CRD declaratively defines a desired [VMAgent](https://docs.victoriametrics.com/vmagent)
setup to run in a Kubernetes cluster.

For each `VMAgent` resource Operator deploys a properly configured `Deployment` in the same namespace.
The VMAgent `Pod`s are configured to mount a `Secret` prefixed with `<VMAgent-name>` containing the configuration
for VMAgent.

For each `VMAgent` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with
name `<VMAgent-name>`.

The CRD specifies which `VMServiceScrape` should be covered by the deployed VMAgent instances based on label selection.
The Operator then generates a configuration based on the included `VMServiceScrape`s and updates the `Secret` which
contains the configuration. It continuously does so for all changes that are made to the `VMServiceScrape`s or the
`VMAgent` resource itself.

If no selection of `VMServiceScrape`s is provided - Operator leaves management of the `Secret` to the user,
so user can set custom configuration while still benefiting from the Operator's capabilities of managing VMAgent setups.

## Specification

You can see the full actual specification of the `VMAgent` resource in the [API docs -> VMAgent](https://docs.victoriametrics.com/operator/api.html#vmagent).

## High availability

<!-- TODO: health checks -->

### Replication and deduplication

To run VMAgent in a highly available manner at first you have to configure deduplication in Victoria Metrics
according [this doc for VMSingle](https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html#deduplication)
or [this doc for VMCluster](https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#deduplication).

You can do it with `extraArgs` on [`VMSingle`](https://docs.victoriametrics.com/operator/resources/vmsingle.html):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-example
spec:
  # ...
  extraArgs:
    dedup.minScrapeInterval: 30s
  # ...
```

For [`VMCluster`](https://docs.victoriametrics.com/operator/resources/vmcluster.html) you can do it with `vmstorage.extraArgs` and `vmselect.extraArgs`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: vmcluster-example
spec:
  # ...
  vmselect:
    extraArgs:
      dedup.minScrapeInterval: 30s
    # ...
  vmstorage:
    extraArgs:
      dedup.minScrapeInterval: 30s
    # ...
```

Deduplication is automatically enabled with `replicationFactor > 1` on `VMCLuster`.

After enabling deduplication you can increase replicas for VMAgent. 

For instance, let's create `VMAgent` with 2 replicas:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-ha-example
spec:
  # ...
  selectAllByDefault: true
  vmAgentExternalLabelName: vmagent-ha
  remoteWrite:
    - url: "http://vmsingle-example.default.svc:8429/api/v1/write"
  # Replication:
  scrapeInterval: 30s
  replicaCount: 2
  # ...
```

Now, even if something happens to one of the vmagent, you'll still have the data.

### StatefulMode

VMAgent supports [persistent buffering](https://docs.victoriametrics.com/vmagent.html#replication-and-high-availability)
for sending data to remote storage. By default, operator set `-remoteWrite.tmpDataPath` for `VMAgent` to `/tmp` (that use k8s ephemeral storage)
and `VMAgent` loses state of the PersistentQueue on pod restarts.

In `StatefulMode` `VMAgent` doesn't lose state of the PersistentQueue (file-based buffer size for unsent data) on pod restarts.
Operator creates `StatefulSet` and, with provided `PersistentVolumeClaimTemplate` at `StatefulStorage` configuration param, metrics queue is stored on disk.

Example of configuration for `StatefulMode`:

```yaml 
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-ha-example
spec:
  # ...
  selectAllByDefault: true
  vmAgentExternalLabelName: vmagent-ha
  remoteWrite:
    - url: "http://vmsingle-example.default.svc:8429/api/v1/write"
  # Replication:
  scrapeInterval: 30s
  replicaCount: 2
  # StatefulMode:
  statefulMode: true
  statefulStorage:
    volumeClaimTemplate:
      spec:
        resources:
            requests:
              storage: 20Gi
  # ...
```

### Sharding

Operator supports sharding with [cluster mode of vmagent](https://docs.victoriametrics.com/vmagent.html#scraping-big-number-of-targets)
for **scraping big number of targets**.

Sharding for `VMAgent` distributes scraping between multiple deployments of `VMAgent`.

Example usage (it is a complete example of `VMAgent` with high availability features):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-ha-example
spec:
  # ...
  selectAllByDefault: true
  vmAgentExternalLabelName: vmagent-ha
  remoteWrite:
    - url: "http://vmsingle-example.default.svc:8429/api/v1/write"
  # Replication:
  scrapeInterval: 30s
  replicaCount: 2
  # StatefulMode:
  statefulMode: true
  statefulStorage:
    volumeClaimTemplate:
      spec:
        resources:
          requests:
            storage: 20Gi
  # Sharding
  shardCount: 5
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - podAffinityTerm:
            labelSelector:
              matchLabels:
                shard-num: '%SHARD_NUM%'
            topologyKey: kubernetes.io/hostname
  # ...
```

This configuration produces `5` deployments with `2` replicas at each. 
Each deployment has its own shard num and scrapes only `1/5` of all targets.

Also, you can use special placeholder `%SHARD_NUM%` in fields of `VMAgent` specification
and operator will replace it with current shard num of vmagent when creating deployment or statefullset for vmagent.

In the example above, the `%SHARD_NUM%` placeholder is used in the `podAntiAffinity` section,
which recommend to scheduler that pods with the same shard num (label `shard-num` in the pod template)
are not deployed on the same node. You can use another `topologyKey` for availability zone or region instead of nodes. 

**Note** that at the moment operator doesn't use `-promscrape.cluster.replicationFactor` parameter of `VMAgent` and 
creates `replicaCount` of replicas for each shard (which leads greater resource consumption). 
This will be fixed in the future, more details can be seen in [this issue](https://github.com/VictoriaMetrics/operator/issues/604).

## Additional scrape configuration

AdditionalScrapeConfigs is an additional way to add scrape targets in `VMAgent` CRD.

There are two options for adding targets into `VMAgent`:

- [inline configuration into CRD](#inline-additional-scrape-configuration-in-vmagent-crd),
- [defining it as a Kubernetes Secret](#define-additional-scrape-configuration-as-a-kubernetes-secret).

No validation happens during the creation of configuration. However, you must validate job specs, and it must follow job spec configuration.
Please check [scrape_configs documentation](https://docs.victoriametrics.com/sd_configs.html#scrape_configs) as references.

### Inline Additional Scrape Configuration in VMAgent CRD

You need to add scrape configuration directly to the `vmagent spec.inlineScrapeConfig`. It is raw text in YAML format.
See example below

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-example
spec:
  # ...
  selectAllByDefault: true
  inlineScrapeConfig: |
    - job_name: "prometheus"
      static_configs:
      - targets: ["localhost:9090"]
  remoteWrite:
    - url: "http://vmsingle-example.default.svc:8429/api/v1/write"
  # ...
```

**Note**: Do not use passwords and tokens with inlineScrapeConfig use Secret instead.

## Define Additional Scrape Configuration as a Kubernetes Secret

You need to define Kubernetes Secret with a key.

The key is `prometheus-additional.yaml` in the example below:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: additional-scrape-configs
stringData:
  prometheus-additional.yaml: |
    - job_name: "prometheus"
      static_configs:
      - targets: ["localhost:9090"]
```

After that, you need to specify the secret's name and key in VMAgent CRD in `additionalScrapeConfigs` section:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-example
spec:
  # ...
  selectAllByDefault: true
  additionalScrapeConfigs:
    name: additional-scrape-configs
    key: prometheus-additional.yaml
  remoteWrite:
    - url: "http://vmsingle-example.default.svc:8429/api/v1/write"
  # ...
```

**Note**: You can specify only one Secret in the VMAgent CRD configuration so use it for all additional scrape configurations.









## Relabeling

`VMAgent` supports global relabeling for all metrics and per remoteWrite target relabel config.

Note in some cases, you don't need relabeling, `key=value` label pairs can be added to the all scrapped metrics with `spec.externalLabels` for `VMAgent`:

```yaml
# simple label add config
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-example
spec:
  externalLabels:
    clusterid: some_cluster
```

`VMAgent` CR supports relabeling with [custom configMap](#relabeling-config-in-configmap) 
or [inline defined at CRD](#inline-relabeling-config).

### Relabeling config in Configmap

Quick tour how to create `ConfigMap` with relabeling configuration:

 ```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vmagent-relabel
data:
  global-relabel.yaml: |
    - target_label: bar
    - source_labels: [aa]
      separator: "foobar"
      regex: "foo.+bar"
      target_label: aaa
      replacement: "xxx"
    - action: keep
      source_labels: [aaa]
    - action: drop
      source_labels: [aaa]
  target-1-relabel.yaml: |
    - action: keep_if_equal
      source_labels: [foo, bar]
    - action: drop_if_equal
      source_labels: [foo, bar]
```

Second, add `relabelConfig` to `VMagent` spec for global relabeling with name of `Configmap` - `vmagent-relabel` and key `global-relabel.yaml`.

For relabeling per remoteWrite target, add   `urlRelabelConfig` name of `Configmap` - `vmagent-relabel` 
and key `target-1-relabel.yaml` to one of remoteWrite target for relabeling only for those target:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-example
spec:
  # ...
  selectAllByDefault: true
  relabelConfig:
   name: "vmagent-relabel"
   key: "global-relabel.yaml"
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
    - url: "http://vmsingle-example-vmsingle.default.svc:8429/api/v1/write"
      urlRelabelConfig:
        name: "vmagent-relabel"
        key: "target-1-relabel.yaml"
```

### Inline relabeling config

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: vmagent-example
spec:
  # ...
  selectAllByDefault: true
  inlineRelabelConfig:
   - target_label: bar
   - source_labels: [aa]
     separator: "foobar"
     regex: "foo.+bar"
     target_label: aaa
     replacement: "xxx"
   - action: keep
     source_labels: [aaa]
   - action: drop
     source_labels: [aaa]
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
    - url: "http://vmsingle-example-vmsingle.default.svc:8429/api/v1/write"
      inlineUrlRelabelConfig:
       - action: keep_if_equal
         source_labels: [foo, bar]
       - action: drop_if_equal
         source_labels: [foo, bar]
```

###  Combined example

It's also possible to use both features in combination.

First will be added relabeling configs from  `inlineRelabelConfig`, then `relabelConfig` from configmap.

 ```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vmagent-relabel
data:
  global-relabel.yaml: |
    - target_label: bar
    - source_labels: [aa]
      separator: "foobar"
      regex: "foo.+bar"
      target_label: aaa
      replacement: "xxx"
    - action: keep
      source_labels: [aaa]
    - action: drop
      source_labels: [aaa]
  target-1-relabel.yaml: |
    - action: keep_if_equal
      source_labels: [foo, bar]
    - action: drop_if_equal
      source_labels: [foo, bar]
```

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  # ...
  selectAllByDefault: true
  inlineRelabelConfig:
   - target_label: bar1
   - source_labels: [aa]
  relabelConfig:
   name: "vmagent-relabel"
   key: "global-relabel.yaml"
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
    - url: "http://vmsingle-example-vmsingle.default.svc:8429/api/v1/write"
      urlRelabelConfig:
        name: "vmagent-relabel"
        key: "target-1-relabel.yaml"
      inlineUrlRelabelConfig:
        - action: keep_if_equal
          source_labels: [foo1, bar2]
```

Resulted configmap, mounted to `VMAgent` pod:

```yaml
apiVersion: v1
data:
  global_relabeling.yaml: |
    - target_label: bar1
    - source_labels:
      - aa
    - target_label: bar
    - source_labels: [aa]
      separator: "foobar"
      regex: "foo.+bar"
      target_label: aaa
      replacement: "xxx"
    - action: keep
      source_labels: [aaa]
    - action: drop
      source_labels: [aaa]
  url_rebaling-1.yaml: |
    - source_labels:
      - foo1
      - bar2
      action: keep_if_equal
    - action: keep_if_equal
      source_labels: [foo, bar]
    - action: drop_if_equal
      source_labels: [foo, bar]
kind: ConfigMap
metadata:
  finalizers:
  - apps.victoriametrics.com/finalizer
  labels:
    app.kubernetes.io/component: monitoring
    app.kubernetes.io/instance: example-vmagent
    app.kubernetes.io/name: vmagent
    managed-by: vm-operator
  name: relabelings-assets-vmagent-example-vmagent
  namespace: default
  ownerReferences:
  - apiVersion: operator.victoriametrics.com/v1beta1
    blockOwnerDeletion: true
    controller: true
    kind: VMAgent
    name: example-vmagent
    uid: 7e9fb838-65da-4443-a43b-c00cd6c4db5b
```

### Additional information

`VMAgent` also has some extra options for relabeling actions, you can check it [docs](https://docs.victoriametrics.com/vmagent#relabeling).
