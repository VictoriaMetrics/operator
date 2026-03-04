---
weight: 13
title: VMSingle
menu:
  docs:
    identifier: operator-cr-vmsingle
    parent: operator-cr
    weight: 13
aliases:
  - /operator/resources/vmsingle/
  - /operator/resources/vmsingle/index.html
tags:
   - kubernetes
   - metrics
---
`VMSingle` represents database for storing metrics.
The `VMSingle` CRD declaratively defines a [single-node VM](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/)
installation to run in a Kubernetes cluster.

For each `VMSingle` resource, the Operator deploys a properly configured `Deployment` in the same namespace.
The VMSingle `Pod`s are configured to mount an empty dir or `PersistentVolumeClaimSpec` for storing data.
Deployment update strategy set to [recreate](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#recreate-deployment).
No more than one replica allowed.

For each `VMSingle` resource, the Operator adds `Service` and `VMServiceScrape` in the same namespace prefixed with name from `VMSingle.metadata.name`.

## Specification

You can see the full actual specification of the `VMSingle` resource in the **[API docs -> VMSingle](https://docs.victoriametrics.com/operator/api/#vmsingle)**.

If you can't find necessary field in the specification of the custom resource,
see [Extra arguments section](https://docs.victoriametrics.com/operator/resources/#extra-arguments).

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vmsingle/#examples) section.

## Scraping

`VMSingle` supports scraping targets with {{% available_from "v0.69.0" "operator" %}}:

- [VMServiceScrape](https://docs.victoriametrics.com/operator/resources/vmservicescrape/)
- [VMPodScrape](https://docs.victoriametrics.com/operator/resources/vmpodscrape/)
- [VMNodeScrape](https://docs.victoriametrics.com/operator/resources/vmnodescrape/)
- [VMStaticScrape](https://docs.victoriametrics.com/operator/resources/vmstaticscrape/)
- [VMProbe](https://docs.victoriametrics.com/operator/resources/vmprobe/)
- [VMScrapeConfig](https://docs.victoriametrics.com/operator/resources/vmscrapeconfig/)

These objects specify which targets VMSingle should scrape and how to collect metrics, and generate part of [VMSingle](https://docs.victoriametrics.com/victoriametrics/vmsingle/) scrape configuration.

To enable scraping on VMSingle, set `spec.ingestOnlyMode` to `false`.

`VMSingle` uses selectors to filter scrape objects. Selectors are defined using the `NamespaceSelector` and `Selector` suffixes for each scrape object type in the VMSingle spec:

- `serviceScrapeNamespaceSelector` and `serviceScrapeSelector` for [VMServiceScrape](https://docs.victoriametrics.com/operator/resources/vmservicescrape/) objects,
- `podScrapeNamespaceSelector` and `podScrapeSelector` for [VMPodScrape](https://docs.victoriametrics.com/operator/resources/vmpodscrape/) objects,
- `probeNamespaceSelector` and `probeSelector` for [VMProbe](https://docs.victoriametrics.com/operator/resources/vmprobe/) objects,
- `staticScrapeNamespaceSelector` and `staticScrapeSelector` for [VMStaticScrape](https://docs.victoriametrics.com/operator/resources/vmstaticscrape/) objects,
- `nodeScrapeNamespaceSelector` and `nodeScrapeSelector` for [VMNodeScrape](https://docs.victoriametrics.com/operator/resources/vmnodescrape/) objects.
- `scrapeConfigNamespaceSelector` and `scrapeConfigSelector` for [VMScrapeConfig](https://docs.victoriametrics.com/operator/resources/vmscrapeconfig/) objects.

This enables access control configuration for objects across namespaces.
See [this doc](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/#labelselector-v1-meta/) for selector specifications.

In addition to these selectors, object filtering in a cluster can be done by the `selectAllByDefault` VMSingle spec field and the operator's `WATCH_NAMESPACE` environment variable.

Following rules are applied:

- If both `...NamespaceSelector` and `...Selector` are undefined, no objects are selected by default. Setting `spec.selectAllByDefault: true` selects all objects of the given type.
- If `...NamespaceSelector` is defined and `...Selector` is undefined, all objects in the namespaces matched by ...NamespaceSelector are selected.
- If `...NamespaceSelector` is undefined and `...Selector` is defined, all objects in VMSingle’s namespaces matching ...Selector are selected.
- If `...NamespaceSelector` and `...Selector` both are defined, then only objects in the namespaces matched by...NamespaceSelector for the given ...Selector are matching.

Below is a more visual and detailed view:

| `...NamespaceSelector` | `...Selector` | `selectAllByDefault` | `WATCH_NAMESPACE` | Selected objects                                                                                            |
|------------------------|---------------|----------------------|-------------------|-------------------------------------------------------------------------------------------------------------|
| undefined              | undefined     | false                | undefined         | nothing                                                                                                     |
| undefined              | undefined     | **true**             | undefined         | all objects of given type (`...`) in the cluster                                                            |
| **defined**            | undefined     | *any*                | undefined         | all objects of given type (`...`) at namespaces for given `...NamespaceSelector`                            |
| undefined              | **defined**   | *any*                | undefined         | all objects of given type (`...`) only at `VMSingle`'s namespace are matching for given `Selector`          |
| **defined**            | **defined**   | *any*                | undefined         | all objects of given type (`...`) only at namespaces matched `...NamespaceSelector` for given `...Selector` |
| *any*                  | undefined     | *any*                | **defined**       | all objects of given type (`...`) only at `VMSingle`'s namespace                                            |
| *any*                  | **defined**   | *any*                | **defined**       | all objects of given type (`...`) only at `VMSingle`'s namespace for given `...Selector`                    |

For more details about the `WATCH_NAMESPACE` variable, see [this doc](https://docs.victoriametrics.com/operator/configuration/#namespaced-mode).

Here are some examples of `VMSingle` configuration with selectors:

```yaml
# select all scrape objects in the cluster
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: select-all
spec:
  # ...
  ingestOnlyMode: false
  selectAllByDefault: true

---
# select all scrape objects in specific namespace (my-namespace)
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: select-ns
spec:
  # ...
  serviceScrapeNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
  podScrapeNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
  nodeScrapeNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
  staticScrapeNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
  probeNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
  scrapeConfigNamespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
```

## High availability

`VMSingle` doesn't support high availability by default, for such purpose
use [`VMCluster`](https://docs.victoriametrics.com/operator/resources/vmcluster/) instead or duplicate the setup.

## Version management

To set `VMSingle` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/VictoriaMetrics/releases)

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/victoria-metrics
    tag: v1.110.13
    pullPolicy: Always
  # ...
```

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example
spec:
  image:
    repository: victoriametrics/victoria-metrics
    tag: v1.110.13
    pullPolicy: Always
  imagePullSecrets:
    - name: my-repo-secret
# ...
```

## Resource management

You can specify resources for each `VMSingle` resource in the `spec` section of the `VMSingle` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
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
by default all `VMSingle` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VMSingle` pods,
- `VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU` - default memory limit for `VMSingle` pods,
- `VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_MEM` - default memory limit for `VMSingle` pods,
- `VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_CPU` - default memory limit for `VMSingle` pods.

These default parameters will be used if:

- `VM_VMSINGLEDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VMSingle` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VMSingle` spec have higher priority than operator parameters.

If you set `VM_VMSINGLEDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VMSingle` CRD,
then `VMSingle` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Enterprise features

VMSingle supports features from [VictoriaMetrics Enterprise](https://docs.victoriametrics.com/victoriametrics/enterprise/#victoriametrics-enterprise):

- [Downsampling](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#downsampling)
- [Multiple retentions / Retention filters](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#retention-filters)
- [Backup automation](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/)

For using Enterprise version of [vmsingle](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/) you need to:
 - specify license at [`spec.license.key`](https://docs.victoriametrics.com/operator/api/#license-key) or at [`spec.license.keyRef`](https://docs.victoriametrics.com/operator/api/#license-keyref).
 - change version of `vmsingle` to version with `-enterprise` suffix using [Version management](https://docs.victoriametrics.com/operator/resources/vmsingle/#version-management).

### Downsampling

After that you can pass [Downsampling](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#downsampling)
flag to `VMSingle` with [extraArgs](https://docs.victoriametrics.com/operator/resources/#extra-arguments) too.

Here are complete example for [Downsampling](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#downsampling):
 
```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
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
    # using enterprise features: Downsampling
    # more details about downsampling you can read on https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#downsampling
    downsampling.period: 30d:5m,180d:1h,1y:6h,2y:1d

  # ...other fields...
```

### Retention filters

The same method is used to enable retention filters - here are complete example for [Retention filters](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#retention-filters).

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
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
    # using enterprise features: Retention filters
    # more details about retention filters you can read on https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#retention-filters
    retentionFilter: '{team="juniors"}:3d,{env=~"dev|staging"}:30d'

  # ...other fields...
```

### Backup automation

You can check [vmbackupmanager documentation](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/) for backup automation.
It contains a description of the service and its features. This section covers vmbackumanager integration in vmoperator.

`VMSingle` has built-in backup configuration, it uses `vmbackupmanager` - proprietary tool for backups.
It supports incremental backups (hourly, daily, weekly, monthly) with popular object storages (aws s3, google cloud storage).

Here is a complete example for backup configuration:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example
spec:
  # enabling enterprise features
  license:
    keyRef:
      name: k8s-secret-that-contains-license
      key: key-in-a-secret-that-contains-license
  image:
    tag: v1.110.13-enterprise
  vmBackup:
    # using enterprise features: Backup automation
    # more details about backup automation you can read on https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/
    destination: "s3://your_bucket/folder"
    credentialsSecret:
      name: remote-storage-keys
      key: credentials

  # ...other fields...

---

apiVersion: v1
kind: Secret
metadata:
  name: remote-storage-keys
type: Opaque
stringData:
  credentials: |-
    [default]
    aws_access_key_id = your_access_key_id
    aws_secret_access_key = your_secret_access_key
``` 

You can read more about backup configuration options and mechanics [here](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/)

Possible configuration options for backup crd can be found at [link](https://docs.victoriametrics.com/operator/api/#vmbackup)

#### Restoring backups

There are several ways to restore with [vmrestore](https://docs.victoriametrics.com/victoriametrics/vmrestore/) or [vmbackupmanager](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/).

##### Manually mounting disk

You have to stop `VMSingle` by scaling it replicas to zero and manually restore data to the database directory.

Steps:

1. Edit `VMSingle` CRD, set `replicaCount: 0`
1. Wait until database stops
1. SSH to some server, where you can mount `VMSingle` disk and mount it manually
1. Restore files with `vmrestore`
1. Umount disk
1. Edit `VMSingle` CRD, set `replicaCount: 1`
1. Wait database start

##### Using VMRestore init container

1. Add init container with `vmrestore` command to `VMSingle` CRD, example:
    ```yaml
    apiVersion: operator.victoriametrics.com/v1beta1
    kind: VMSingle
    metadata:
      name: vmsingle
    spec:
      # enabling enterprise features
      license:
        keyRef:
          name: k8s-secret-that-contains-license
          key: key-in-a-secret-that-contains-license
      image:
        tag: v1.110.13-enterprise
      vmBackup:
        # using enterprise features: Backup automation
        # more details about backup automation you can read https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/
        destination: "s3://your_bucket/folder"
        credentialsSecret:
          name: remote-storage-keys
          key: credentials
          
      extraArgs:
        runOnStart: "true"
        
      initContainers:
        - name: vmrestore
          image: victoriametrics/vmrestore:latest
          volumeMounts:
            - mountPath: /victoria-metrics-data
              name: data
            - mountPath: /etc/vm/creds
              name: secret-remote-storage-keys
              readOnly: true
          args:
            - -storageDataPath=/victoria-metrics-data
            - -src=s3://your_bucket/folder/latest
            - -credsFilePath=/etc/vm/creds/credentials
    
      # ...other fields...
    ```
1. Apply it, and db will be restored from S3
1. Remove `initContainers` and apply CRD.

Note that using `VMRestore` will require adjusting `src` for each pod because restore will be handled per-pod.

##### Using VMBackupmanager init container

Using VMBackupmanager restore in Kubernetes environment is described [here](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/#how-to-restore-in-kubernetes).

Advantages of using `VMBackupmanager` include:

- Automatic adjustment of `src` for each pod when backup is requested
- Graceful handling of case when no restore is required - `VMBackupmanager` will exit with successful status code and won't prevent pod from starting

## Examples

```yaml
kind: VMSingle
metadata:
  name: example
spec:
  retentionPeriod: "12"
  removePvcAfterDelete: true
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 50Gi
  extraArgs:
    dedup.minScrapeInterval: 60s
  resources:
    requests:
      memory: 500Mi
      cpu: 500m
    limits:
      memory: 10Gi
      cpu: 5
```
