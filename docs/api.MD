
# API Docs

This Document documents the types introduced by the Victoria Metrics Operator to be consumed by users.

> Note this document is generated from code comments. When contributing a change to this document please do so by changing the code comments.

## Table of Contents
* [Alertmanager](#alertmanager)
* [AlertmanagerList](#alertmanagerlist)
* [AlertmanagerSpec](#alertmanagerspec)
* [AlertmanagerStatus](#alertmanagerstatus)
* [VmAgent](#vmagent)
* [VmAgentList](#vmagentlist)
* [VmAgentSpec](#vmagentspec)
* [VmAgentStatus](#vmagentstatus)
* [EmbeddedObjectMetadata](#embeddedobjectmetadata)
* [EmbeddedPersistentVolumeClaim](#embeddedpersistentvolumeclaim)
* [RemoteSpec](#remotespec)
* [StorageSpec](#storagespec)
* [VmAlert](#vmalert)
* [VmAlertList](#vmalertlist)
* [VmAlertSpec](#vmalertspec)
* [VmAlertStatus](#vmalertstatus)
* [VmSingle](#vmsingle)
* [VmSingleList](#vmsinglelist)
* [VmSingleSpec](#vmsinglespec)
* [VmSingleStatus](#vmsinglestatus)
* [PrometheusRule](#prometheusrule)
* [PrometheusRuleList](#prometheusrulelist)
* [PrometheusRuleSpec](#prometheusrulespec)
* [Rule](#rule)
* [RuleGroup](#rulegroup)
* [APIServerConfig](#apiserverconfig)
* [ArbitraryFSAccessThroughSMsConfig](#arbitraryfsaccessthroughsmsconfig)
* [BasicAuth](#basicauth)
* [Endpoint](#endpoint)
* [NamespaceSelector](#namespaceselector)
* [PodMetricsEndpoint](#podmetricsendpoint)
* [PodMonitor](#podmonitor)
* [PodMonitorList](#podmonitorlist)
* [PodMonitorSpec](#podmonitorspec)
* [QueueConfig](#queueconfig)
* [RelabelConfig](#relabelconfig)
* [RemoteWriteSpec](#remotewritespec)
* [SecretOrConfigMap](#secretorconfigmap)
* [ServiceMonitor](#servicemonitor)
* [ServiceMonitorList](#servicemonitorlist)
* [ServiceMonitorSpec](#servicemonitorspec)
* [TLSConfig](#tlsconfig)

## Alertmanager

Alertmanager describes an Alertmanager cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of the desired behavior of the Alertmanager cluster. More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | [AlertmanagerSpec](#alertmanagerspec) | true |
| status | Most recent observed status of the Alertmanager cluster. Read-only. Not included when requesting from the apiserver, only from the Prometheus Operator API itself. More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status | *[AlertmanagerStatus](#alertmanagerstatus) | false |

[Back to TOC](#table-of-contents)

## AlertmanagerList

AlertmanagerList is a list of Alertmanagers.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata | Standard list metadata More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items | List of Alertmanagers | [][Alertmanager](#alertmanager) | true |

[Back to TOC](#table-of-contents)

## AlertmanagerSpec

AlertmanagerSpec is a specification of the desired behavior of the Alertmanager cluster. More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| podMetadata | PodMetadata configures Labels and Annotations which are propagated to the alertmanager pods. | *[EmbeddedObjectMetadata](#embeddedobjectmetadata) | false |
| image | Image if specified has precedence over baseImage, tag and sha combinations. Specifying the version is still necessary to ensure the Prometheus Operator knows what version of Alertmanager is being configured. | *string | false |
| version | Version the cluster should be on. | string | false |
| tag | Tag of Alertmanager container image to be deployed. Defaults to the value of `version`. Version is ignored if Tag is set. | string | false |
| sha | SHA of Alertmanager container image to be deployed. Defaults to the value of `version`. Similar to a tag, but the SHA explicitly deploys an immutable container image. Version and Tag are ignored if SHA is set. | string | false |
| baseImage | BaseImage that is used to deploy pods, without tag. | string | false |
| imagePullSecrets | ImagePullSecrets An optional list of references to secrets in the same namespace to use for pulling prometheus and alertmanager images from registries see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod | [][v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#localobjectreference-v1-core) | false |
| secrets | Secrets is a list of Secrets in the same namespace as the Alertmanager object, which shall be mounted into the Alertmanager Pods. The Secrets are mounted into /etc/alertmanager/secrets/<secret-name>. | []string | false |
| configMaps | ConfigMaps is a list of ConfigMaps in the same namespace as the Alertmanager object, which shall be mounted into the Alertmanager Pods. The ConfigMaps are mounted into /etc/alertmanager/configmaps/<configmap-name>. | []string | false |
| configSecret | ConfigSecret is the name of a Kubernetes Secret in the same namespace as the Alertmanager object, which contains configuration for this Alertmanager instance. Defaults to 'alertmanager-<alertmanager-name>' The secret is mounted into /etc/alertmanager/config. | string | false |
| logLevel | Log level for Alertmanager to be configured with. | string | false |
| logFormat | LogFormat for Alertmanager to be configured with. | string | false |
| replicas | Replicas Size is the expected size of the alertmanager cluster. The controller will eventually make the size of the running cluster equal to the expected | *int32 | false |
| retention | Retention Time duration Alertmanager shall retain data for. Default is '120h', and must match the regular expression `[0-9]+(ms\|s\|m\|h)` (milliseconds seconds minutes hours). | string | false |
| storage | Storage is the definition of how storage will be used by the Alertmanager instances. | *[StorageSpec](#storagespec) | false |
| volumes | Volumes allows configuration of additional volumes on the output StatefulSet definition. Volumes specified will be appended to other volumes that are generated as a result of StorageSpec objects. | [][v1.Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | false |
| volumeMounts | VolumeMounts allows configuration of additional VolumeMounts on the output StatefulSet definition. VolumeMounts specified will be appended to other VolumeMounts in the alertmanager container, that are generated as a result of StorageSpec objects. | [][v1.VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volumemount-v1-core) | false |
| externalURL | ExternalURL the Alertmanager instances will be available under. This is necessary to generate correct URLs. This is necessary if Alertmanager is not served from root of a DNS name. | string | false |
| routePrefix | RoutePrefix Alertmanager registers HTTP handlers for. This is useful, if using ExternalURL and a proxy is rewriting HTTP routes of a request, and the actual ExternalURL is still true, but the server serves requests under a different route prefix. For example for use with `kubectl proxy`. | string | false |
| paused | Paused If set to true all actions on the underlaying managed objects are not goint to be performed, except for delete actions. | bool | false |
| nodeSelector | NodeSelector Define which Nodes the Pods are scheduled on. | map[string]string | false |
| resources | Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ | [v1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | false |
| affinity | Affinity If specified, the pod's scheduling constraints. | *[v1.Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | false |
| tolerations | Tolerations If specified, the pod's tolerations. | [][v1.Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#toleration-v1-core) | false |
| securityContext | SecurityContext holds pod-level security attributes and common container settings. This defaults to the default PodSecurityContext. | *v1.PodSecurityContext | false |
| serviceAccountName | ServiceAccountName is the name of the ServiceAccount to use to run the Prometheus Pods. | string | false |
| listenLocal | ListenLocal makes the Alertmanager server listen on loopback, so that it does not bind against the Pod IP. Note this is only for the Alertmanager UI, not the gossip communication. | bool | false |
| containers | Containers allows injecting additional containers. This is meant to allow adding an authentication proxy to an Alertmanager pod. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| initContainers | InitContainers allows adding initContainers to the pod definition. Those can be used to e.g. fetch secrets for injection into the Alertmanager configuration from external sources. Any errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/ Using initContainers for any use case other then secret fetching is entirely outside the scope of what the maintainers will support and by doing so, you accept that this behaviour may break at any time without notice. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| priorityClassName | PriorityClassName class assigned to the Pods | string | false |
| additionalPeers | AdditionalPeers allows injecting a set of additional Alertmanagers to peer with to form a highly available cluster. | []string | false |
| clusterAdvertiseAddress | ClusterAdvertiseAddress is the explicit address to advertise in cluster. Needs to be provided for non RFC1918 [1] (public) addresses. [1] RFC1918: https://tools.ietf.org/html/rfc1918 | string | false |
| portName | PortName used for the pods and governing service. This defaults to web | string | false |

[Back to TOC](#table-of-contents)

## AlertmanagerStatus

AlertmanagerStatus is the most recent observed status of the Alertmanager cluster. Read-only. Not included when requesting from the apiserver, only from the Prometheus Operator API itself. More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| paused | Paused Represents whether any actions on the underlaying managed objects are being performed. Only delete actions will be performed. | bool | true |
| replicas | Replicas Total number of non-terminated pods targeted by this Alertmanager cluster (their labels match the selector). | int32 | true |
| updatedReplicas | UpdatedReplicas Total number of non-terminated pods targeted by this Alertmanager cluster that have the desired version spec. | int32 | true |
| availableReplicas | AvailableReplicas Total number of available pods (ready for at least minReadySeconds) targeted by this Alertmanager cluster. | int32 | true |
| unavailableReplicas | UnavailableReplicas Total number of unavailable pods targeted by this Alertmanager cluster. | int32 | true |

[Back to TOC](#table-of-contents)

## VmAgent

VmAgent is the Schema for the vmagents API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec |  | [VmAgentSpec](#vmagentspec) | false |
| status |  | [VmAgentStatus](#vmagentstatus) | false |

[Back to TOC](#table-of-contents)

## VmAgentList

VmAgentList contains a list of VmAgent

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][VmAgent](#vmagent) | true |

[Back to TOC](#table-of-contents)

## VmAgentSpec

VmAgentSpec defines the desired state of VmAgent

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| podMetadata | PodMetadata configures Labels and Annotations which are propagated to the vmagent pods. | *[EmbeddedObjectMetadata](#embeddedobjectmetadata) | false |
| image | Image - victoria metrics agent base image if not specified - use default from operator config | *string | false |
| version | Version for VmAgent. | string | false |
| imagePullSecrets | ImagePullSecrets optional list of references to secrets in the same namespace to use for pulling prometheus and vmagent images from registries see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod | [][v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#localobjectreference-v1-core) | false |
| secrets | Secrets is a list of Secrets in the same namespace as the vmagent object, which shall be mounted into the vmagent Pods. will be mounted at path /etc/vmagent/secrets | []string | false |
| configMaps | ConfigMaps is a list of ConfigMaps in the same namespace as the vmagent object, which shall be mounted into the vmagent Pods. will be mounted at path  /etc/vmagent/configs | []string | false |
| logLevel | LogLevel for VmAgent to be configured with. INFO, WARN, ERROR, FATAL, PANIC | string | false |
| logFormat | LogFormat for VmAgent to be configured with. | string | false |
| replicas | Replicas is the expected size of the VmAgent cluster. The controller will eventually make the size of the running cluster equal to the expected size. NOTE enable VmSingle deduplication for replica usage | *int32 | true |
| volumes | Volumes allows configuration of additional volumes on the output deploy definition. Volumes specified will be appended to other volumes that are generated as a result of StorageSpec objects. | [][v1.Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | false |
| volumeMounts | VolumeMounts allows configuration of additional VolumeMounts on the output deploy definition. VolumeMounts specified will be appended to other VolumeMounts in the vmagent container, that are generated as a result of StorageSpec objects. | [][v1.VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volumemount-v1-core) | false |
| resources | Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ if not specified - default setting will be used | [v1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | false |
| affinity | Affinity If specified, the pod's scheduling constraints. | *[v1.Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | false |
| tolerations | Tolerations If specified, the pod's tolerations. | [][v1.Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#toleration-v1-core) | false |
| securityContext | SecurityContext holds pod-level security attributes and common container settings. This defaults to the default PodSecurityContext. | *v1.PodSecurityContext | false |
| serviceAccountName | ServiceAccountName is the name of the ServiceAccount to use to run the VmAgent Pods. required | string | true |
| containers | Containers property allows to inject additions sidecars. It can be useful for proxies, backup, etc. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| initContainers | InitContainers allows adding initContainers to the pod definition. Those can be used to e.g. fetch secrets for injection into the vmagent configuration from external sources. Any errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/ Using initContainers for any use case other then secret fetching is entirely outside the scope of what the maintainers will support and by doing so, you accept that this behaviour may break at any time without notice. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| priorityClassName | PriorityClassName assigned to the Pods | string | false |
| scrapeInterval | ScrapeInterval defines how often scrape targets by default | string | false |
| aPIServerConfig | APIServerConfig allows specifying a host and auth methods to access apiserver. If left empty, VmAgent is assumed to run inside of the cluster and will discover API servers automatically and use the pod's CA certificate and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/. | *monitoringv1.APIServerConfig | false |
| overrideHonorLabels | OverrideHonorLabels if set to true overrides all user configured honor_labels. If HonorLabels is set in ServiceMonitor or PodMonitor to true, this overrides honor_labels to false. | bool | false |
| overrideHonorTimestamps | OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs. | bool | false |
| ignoreNamespaceSelectors | IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from the podmonitor and servicemonitor configs, and they will only discover endpoints within their current namespace.  Defaults to false. | bool | false |
| enforcedNamespaceLabel | EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert and metric that is user created. The label value will always be the namespace of the object that is being created. | string | false |
| vmAgentExternalLabelName | VmAgentExternalLabelName Name of vmAgent external label used to denote vmAgent instance name. Defaults to the value of `prometheus`. External label will _not_ be added when value is set to empty string (`\"\"`). | *string | false |
| replicaExternalLabelName | ReplicaExternalLabelName Name of vmagent external label used to denote replica name. Defaults to the value of `prometheus_replica`. External label will _not_ be added when value is set to empty string (`\"\"`). | *string | false |
| externalLabels | ExternalLabels The labels to add to any time series or alerts when communicating with external systems (federation, remote storage, etc). | map[string]string | false |
| remoteWrite | RemoteWrite list of victoria metrics /some other remote write system for vm it must looks like: http://victoria-metrics-single:8429/api/v1/write or for cluster different url https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmagent#splitting-data-streams-among-multiple-systems | [][RemoteSpec](#remotespec) | true |
| serviceMonitorSelector | ServiceMonitorSelector defines ServiceMonitors to be selected for target discovery. if neither this nor podMonitorSelector are specified, configuration is unmanaged. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | false |
| serviceMonitorNamespaceSelector | ServiceMonitorNamespaceSelector Namespaces to be selected for ServiceMonitor discovery. If nil, only check own namespace. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | false |
| podMonitorSelector | PodMonitorSelector defines PodMonitors to be selected for target discovery. if neither this nor serviceMonitorSelector are specified, configuration is unmanaged. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | false |
| podMonitorNamespaceSelector | PodMonitorNamespaceSelector defines Namespaces to be selected for PodMonitor discovery. If nil, only check own namespace. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | false |
| additionalScrapeConfigs | AdditionalScrapeConfigs As scrape configs are appended, the user is responsible to make sure it is valid. Note that using this feature may expose the possibility to break upgrades of VmAgent. It is advised to review VmAgent release notes to ensure that no incompatible scrape configs are going to break VmAgent after the upgrade. | *[v1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#secretkeyselector-v1-core) | false |
| arbitraryFSAccessThroughSMs | ArbitraryFSAccessThroughSMs configures whether configuration based on a service monitor can access arbitrary files on the file system of the VmAgent container e.g. bearer token files. | monitoringv1.ArbitraryFSAccessThroughSMsConfig | false |
| port | Port listen address | string | false |
| extraArgs | ExtraArgs that will be passed to  VmAlert pod for example -remoteWrite.tmpDataPath=/tmp | []string | false |
| extraEnvs | ExtraEnvs that will be added to VmAlert pod | [][v1.EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core) | false |

[Back to TOC](#table-of-contents)

## VmAgentStatus

VmAgentStatus defines the observed state of VmAgent

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| replicas | Replicas Total number of non-terminated pods targeted by this VmAlert cluster (their labels match the selector). | int32 | true |
| updatedReplicas | UpdatedReplicas Total number of non-terminated pods targeted by this VmAlert cluster that have the desired version spec. | int32 | true |
| availableReplicas | AvailableReplicas Total number of available pods (ready for at least minReadySeconds) targeted by this VmAlert cluster. | int32 | true |
| unavailableReplicas | UnavailableReplicas Total number of unavailable pods targeted by this VmAlert cluster. | int32 | true |

[Back to TOC](#table-of-contents)

## EmbeddedObjectMetadata

EmbeddedObjectMetadata contains a subset of the fields included in k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta Only fields which are relevant to embedded resources are included.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name | Name must be unique within a namespace. Is required when creating resources, although some resources may allow a client to request the generation of an appropriate name automatically. Name is primarily intended for creation idempotence and configuration definition. Cannot be updated. More info: http://kubernetes.io/docs/user-guide/identifiers#names | string | false |
| labels | Labels Map of string keys and values that can be used to organize and categorize (scope and select) objects. May match selectors of replication controllers and services. More info: http://kubernetes.io/docs/user-guide/labels | map[string]string | false |
| annotations | Annotations is an unstructured key value map stored with a resource that may be set by external tools to store and retrieve arbitrary metadata. They are not queryable and should be preserved when modifying objects. More info: http://kubernetes.io/docs/user-guide/annotations | map[string]string | false |

[Back to TOC](#table-of-contents)

## EmbeddedPersistentVolumeClaim

EmbeddedPersistentVolumeClaim is an embedded version of k8s.io/api/core/v1.PersistentVolumeClaim. It contains TypeMeta and a reduced ObjectMeta.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata | EmbeddedMetadata contains metadata relevant to an EmbeddedResource. | [EmbeddedObjectMetadata](#embeddedobjectmetadata) | false |
| spec | Spec defines the desired characteristics of a volume requested by a pod author. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims | [v1.PersistentVolumeClaimSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#persistentvolumeclaimspec-v1-core) | false |
| status | Status represents the current information/status of a persistent volume claim. Read-only. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims | v1.PersistentVolumeClaimStatus | false |

[Back to TOC](#table-of-contents)

## RemoteSpec

RemoteWriteSpec defines the remote_write configuration for prometheus.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| url | URL of the endpoint to send samples to. | string | true |

[Back to TOC](#table-of-contents)

## StorageSpec

StorageSpec defines the configured storage for a group Prometheus servers. If neither `emptyDir` nor `volumeClaimTemplate` is specified, then by default an [EmptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) will be used.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| disableMountSubPath | Deprecated: subPath usage will be disabled by default in a future release, this option will become unnecessary. DisableMountSubPath allows to remove any subPath usage in volume mounts. | bool | false |
| emptyDir | EmptyDirVolumeSource to be used by the Prometheus StatefulSets. If specified, used in place of any volumeClaimTemplate. More info: https://kubernetes.io/docs/concepts/storage/volumes/#emptydir | *[v1.EmptyDirVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#emptydirvolumesource-v1-core) | false |
| volumeClaimTemplate | A PVC spec to be used by the AlertManager StatefulSets. | [EmbeddedPersistentVolumeClaim](#embeddedpersistentvolumeclaim) | false |

[Back to TOC](#table-of-contents)

## VmAlert

VmAlert is the Schema for the vmalerts API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec |  | [VmAlertSpec](#vmalertspec) | false |
| status |  | [VmAlertStatus](#vmalertstatus) | false |

[Back to TOC](#table-of-contents)

## VmAlertList

VmAlertList contains a list of VmAlert

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][VmAlert](#vmalert) | true |

[Back to TOC](#table-of-contents)

## VmAlertSpec

VmAlertSpec defines the desired state of VmAlert

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| podMetadata | PodMetadata configures Labels and Annotations which are propagated to the VmAlert pods. | *[EmbeddedObjectMetadata](#embeddedobjectmetadata) | false |
| image | Image victoria metrics alert base image | *string | false |
| version | Version the VmAlert should be on. | string | false |
| imagePullSecrets | ImagePullSecrets An optional list of references to secrets in the same namespace to use for pulling prometheus and VmAlert images from registries see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod | [][v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#localobjectreference-v1-core) | false |
| secrets | Secrets is a list of Secrets in the same namespace as the VmAlert object, which shall be mounted into the VmAlert Pods. The Secrets are mounted into /etc/vmalert/secrets/<secret-name>. | []string | false |
| configMaps | ConfigMaps is a list of ConfigMaps in the same namespace as the VmAlert object, which shall be mounted into the VmAlert Pods. The ConfigMaps are mounted into /etc/vmalert/configmaps/<configmap-name>. | []string | false |
| logFormat | LogFormat for VmAlert to be configured with. default or json | string | false |
| logLevel | LogLevel for VmAlert to be configured with. | string | false |
| replicas | Replicas is the expected size of the VmAlert cluster. The controller will eventually make the size of the running cluster equal to the expected size. | *int32 | true |
| volumes | Volumes allows configuration of additional volumes on the output Deployment definition. Volumes specified will be appended to other volumes that are generated as a result of StorageSpec objects. | [][v1.Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | false |
| volumeMounts | VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition. VolumeMounts specified will be appended to other VolumeMounts in the VmAlert container, that are generated as a result of StorageSpec objects. | [][v1.VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volumemount-v1-core) | false |
| resources | Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ | [v1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | false |
| affinity | Affinity If specified, the pod's scheduling constraints. | *[v1.Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | false |
| tolerations | Tolerations If specified, the pod's tolerations. | [][v1.Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#toleration-v1-core) | false |
| securityContext | SecurityContext holds pod-level security attributes and common container settings. This defaults to the default PodSecurityContext. | *v1.PodSecurityContext | false |
| serviceAccountName | ServiceAccountName is the name of the ServiceAccount to use to run the VmAlert Pods. | string | false |
| containers | Containers property allows to inject additions sidecars. It can be useful for proxies, backup, etc. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| initContainers | InitContainers allows adding initContainers to the pod definition. Those can be used to e.g. fetch secrets for injection into the VmAlert configuration from external sources. Any errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/ Using initContainers for any use case other then secret fetching is entirely outside the scope of what the maintainers will support and by doing so, you accept that this behaviour may break at any time without notice. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| priorityClassName | Priority class assigned to the Pods | string | false |
| evaluationInterval | EvaluationInterval how often evalute rules by default | string | false |
| enforcedNamespaceLabel | EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert and metric that is user created. The label value will always be the namespace of the object that is being created. | string | false |
| ruleSelector | RuleSelector selector to select which PrometheusRules to mount for loading alerting rules from. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | false |
| ruleNamespaceSelector | RuleNamespaceSelector to be selected for PrometheusRules discovery. If unspecified, only the same namespace as the Prometheus object is in is used. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | false |
| port | Port for listen | string | false |
| notifierURL | NotifierURL prometheus alertmanager URL. Required parameter. e.g. http://127.0.0.1:9093 | string | true |
| remoteWrite | RemoteWrite Optional URL to remote-write compatible storage where to write timeseriesbased on active alerts. E.g. http://127.0.0.1:8428 | [RemoteSpec](#remotespec) | false |
| remoteRead | RemoteRead victoria metrics address for loading state This configuration makes sense only if remoteWrite was configured before and has been successfully persisted its state. | [RemoteSpec](#remotespec) | false |
| rulePath | RulePath to the file with alert rules. Supports patterns. Flag can be specified multiple times. Examples:\n        -rule /path/to/file. Path to a single file with alerting rules\n        -rule dir/*.yaml -rule /*.yaml. Relative path to all .yaml files in \"dir\" folder,\n       absolute path to all .yaml files in root.\nby default operator adds /etc/vmalert/configs/base/vmalert.yaml | []string | false |
| datasource | Datasource Victoria Metrics or VMSelect url. Required parameter. e.g. http://127.0.0.1:8428 | [RemoteSpec](#remotespec) | true |
| extraArgs | ExtraArgs that will be passed to  VmAlert pod for example -remoteWrite.tmpDataPath=/tmp | []string | false |
| extraEnvs | ExtraEnvs that will be added to VmAlert pod | [][v1.EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core) | false |

[Back to TOC](#table-of-contents)

## VmAlertStatus

VmAlertStatus defines the observed state of VmAlert

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| replicas | Replicas Total number of non-terminated pods targeted by this VmAlert cluster (their labels match the selector). | int32 | true |
| updatedReplicas | UpdatedReplicas Total number of non-terminated pods targeted by this VmAlert cluster that have the desired version spec. | int32 | true |
| availableReplicas | AvailableReplicas Total number of available pods (ready for at least minReadySeconds) targeted by this VmAlert cluster. | int32 | true |
| unavailableReplicas | UnavailableReplicas Total number of unavailable pods targeted by this VmAlert cluster. | int32 | true |

[Back to TOC](#table-of-contents)

## VmSingle

VmSingle is the Schema for the vmsingles API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec |  | [VmSingleSpec](#vmsinglespec) | false |
| status |  | [VmSingleStatus](#vmsinglestatus) | false |

[Back to TOC](#table-of-contents)

## VmSingleList

VmSingleList contains a list of VmSingle

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][VmSingle](#vmsingle) | true |

[Back to TOC](#table-of-contents)

## VmSingleSpec

VmSingleSpec defines the desired state of VmSingle

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| podMetadata | PodMetadata configures Labels and Annotations which are propagated to the VmSingle pods. | *[EmbeddedObjectMetadata](#embeddedobjectmetadata) | false |
| image | Image victoria metrics single base image | *string | false |
| version | Version of victoria metrics single | string | false |
| imagePullSecrets | ImagePullSecrets An optional list of references to secrets in the same namespace to use for pulling prometheus and VmSingle images from registries see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod | [][v1.LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#localobjectreference-v1-core) | false |
| secrets | Secrets is a list of Secrets in the same namespace as the VmSingle object, which shall be mounted into the VmSingle Pods. | []string | false |
| configMaps | ConfigMaps is a list of ConfigMaps in the same namespace as the VmSingle object, which shall be mounted into the VmSingle Pods. | []string | false |
| logLevel | LogLevel for victoria metrics single to be configured with. | string | false |
| logFormat | LogFormat for VmSingle to be configured with. | string | false |
| replicas | Replicas is the expected size of the VmSingle it can be 0 or 1 if you need more - use vm cluster | *int32 | false |
| storage | Storage is the definition of how storage will be used by the VmSingle by default it`s empty dir | *[v1.PersistentVolumeClaimSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#persistentvolumeclaimspec-v1-core) | false |
| volumes | Volumes allows configuration of additional volumes on the output deploy definition. Volumes specified will be appended to other volumes that are generated as a result of StorageSpec objects. | [][v1.Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | false |
| volumeMounts | VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition. VolumeMounts specified will be appended to other VolumeMounts in the VmSingle container, that are generated as a result of StorageSpec objects. | [][v1.VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volumemount-v1-core) | false |
| resources | Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ if not defined default resources from operator config will be used | [v1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | false |
| affinity | Affinity If specified, the pod's scheduling constraints. | *[v1.Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | false |
| tolerations | Tolerations If specified, the pod's tolerations. | [][v1.Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#toleration-v1-core) | false |
| securityContext | SecurityContext holds pod-level security attributes and common container settings. This defaults to the default PodSecurityContext. | *v1.PodSecurityContext | false |
| serviceAccountName | ServiceAccountName is the name of the ServiceAccount to use to run the VmSingle Pods. | string | false |
| containers | Containers property allows to inject additions sidecars. It can be useful for proxies, backup, etc. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| initContainers | InitContainers allows adding initContainers to the pod definition. Those can be used to e.g. fetch secrets for injection into the vmSingle configuration from external sources. Any errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/ Using initContainers for any use case other then secret fetching is entirely outside the scope of what the maintainers will support and by doing so, you accept that this behaviour may break at any time without notice. | [][v1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| priorityClassName | PriorityClassName assigned to the Pods | string | false |
| port | Port listen port | string | false |
| removePvcAfterDelete | RemovePvcAfterDelete - if true, controller adds ownership to pvc and after VmSingle objest deletion - pvc will be garbage collected by controller manager | bool | false |
| retentionPeriod | RetentionPeriod in months | string | true |
| extraArgs | ExtraArgs that will be passed to  VmSingle pod for example -remoteWrite.tmpDataPath=/tmp | []string | false |
| extraEnvs | ExtraEnvs that will be added to VmSingle pod | [][v1.EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core) | false |

[Back to TOC](#table-of-contents)

## VmSingleStatus

VmSingleStatus defines the observed state of VmSingle

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| replicas | Replicas Total number of non-terminated pods targeted by this VmAlert cluster (their labels match the selector). | int32 | true |
| updatedReplicas | UpdatedReplicas Total number of non-terminated pods targeted by this VmAlert cluster that have the desired version spec. | int32 | true |
| availableReplicas | AvailableReplicas Total number of available pods (ready for at least minReadySeconds) targeted by this VmAlert cluster. | int32 | true |
| unavailableReplicas | UnavailableReplicas Total number of unavailable pods targeted by this VmAlert cluster. | int32 | true |

[Back to TOC](#table-of-contents)

## PrometheusRule

PrometheusRule defines alerting rules for a Prometheus instance

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of desired alerting rule definitions for Prometheus. | [PrometheusRuleSpec](#prometheusrulespec) | true |

[Back to TOC](#table-of-contents)

## PrometheusRuleList

PrometheusRuleList is a list of PrometheusRules.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata | Standard list metadata More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items | List of Rules | []*[PrometheusRule](#prometheusrule) | true |

[Back to TOC](#table-of-contents)

## PrometheusRuleSpec

PrometheusRuleSpec contains specification parameters for a Rule.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| groups | Content of Prometheus rule file | [][RuleGroup](#rulegroup) | false |

[Back to TOC](#table-of-contents)

## Rule

Rule describes an alerting or recording rule.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| record |  | string | false |
| alert |  | string | false |
| expr |  | intstr.IntOrString | true |
| for |  | string | false |
| labels |  | map[string]string | false |
| annotations |  | map[string]string | false |

[Back to TOC](#table-of-contents)

## RuleGroup

RuleGroup is a list of sequentially evaluated recording and alerting rules. Note: PartialResponseStrategy is only used by ThanosRuler and will be ignored by Prometheus instances.  Valid values for this field are 'warn' or 'abort'.  More info: https://github.com/thanos-io/thanos/blob/master/docs/components/rule.md#partial-response

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name |  | string | true |
| interval |  | string | false |
| rules |  | [][Rule](#rule) | true |
| partialResponseStrategy |  | string | false |

[Back to TOC](#table-of-contents)

## APIServerConfig

APIServerConfig defines a host and auth methods to access apiserver. More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| host | Host of apiserver. A valid string consisting of a hostname or IP followed by an optional port number | string | true |
| basicAuth | BasicAuth allow an endpoint to authenticate over basic authentication | *[BasicAuth](#basicauth) | false |
| bearerToken | Bearer token for accessing apiserver. | string | false |
| bearerTokenFile | File to read bearer token for accessing apiserver. | string | false |
| tlsConfig | TLS Config to use for accessing apiserver. | *[TLSConfig](#tlsconfig) | false |

[Back to TOC](#table-of-contents)

## ArbitraryFSAccessThroughSMsConfig

ArbitraryFSAccessThroughSMsConfig enables users to configure, whether a service monitor selected by the Prometheus instance is allowed to use arbitrary files on the file system of the Prometheus container. This is the case when e.g. a service monitor specifies a BearerTokenFile in an endpoint. A malicious user could create a service monitor selecting arbitrary secret files in the Prometheus container. Those secrets would then be sent with a scrape request by Prometheus to a malicious target. Denying the above would prevent the attack, users can instead use the BearerTokenSecret field.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| deny |  | bool | false |

[Back to TOC](#table-of-contents)

## BasicAuth

BasicAuth allow an endpoint to authenticate over basic authentication More info: https://prometheus.io/docs/operating/configuration/#endpoints

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| username | The secret in the service monitor namespace that contains the username for authentication. | [v1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#secretkeyselector-v1-core) | false |
| password | The secret in the service monitor namespace that contains the password for authentication. | [v1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#secretkeyselector-v1-core) | false |

[Back to TOC](#table-of-contents)

## Endpoint

Endpoint defines a scrapeable endpoint serving Prometheus metrics.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| port | Name of the service port this endpoint refers to. Mutually exclusive with targetPort. | string | false |
| targetPort | Name or number of the pod port this endpoint refers to. Mutually exclusive with port. | *intstr.IntOrString | false |
| path | HTTP path to scrape for metrics. | string | false |
| scheme | HTTP scheme to use for scraping. | string | false |
| params | Optional HTTP URL parameters | map[string][]string | false |
| interval | Interval at which metrics should be scraped | string | false |
| scrapeTimeout | Timeout after which the scrape is ended | string | false |
| tlsConfig | TLS configuration to use when scraping the endpoint | *[TLSConfig](#tlsconfig) | false |
| bearerTokenFile | File to read bearer token for scraping targets. | string | false |
| bearerTokenSecret | Secret to mount to read bearer token for scraping targets. The secret needs to be in the same namespace as the service monitor and accessible by the Prometheus Operator. | [v1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#secretkeyselector-v1-core) | false |
| honorLabels | HonorLabels chooses the metric's labels on collisions with target labels. | bool | false |
| honorTimestamps | HonorTimestamps controls whether Prometheus respects the timestamps present in scraped data. | *bool | false |
| basicAuth | BasicAuth allow an endpoint to authenticate over basic authentication More info: https://prometheus.io/docs/operating/configuration/#endpoints | *[BasicAuth](#basicauth) | false |
| metricRelabelConfigs | MetricRelabelConfigs to apply to samples before ingestion. | []*[RelabelConfig](#relabelconfig) | false |
| relabelConfigs | RelabelConfigs to apply to samples before scraping. More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config | []*[RelabelConfig](#relabelconfig) | false |
| proxyURL | ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint. | *string | false |

[Back to TOC](#table-of-contents)

## NamespaceSelector

NamespaceSelector is a selector for selecting either all namespaces or a list of namespaces.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| any | Boolean describing whether all namespaces are selected in contrast to a list restricting them. | bool | false |
| matchNames | List of namespace names. | []string | false |

[Back to TOC](#table-of-contents)

## PodMetricsEndpoint

PodMetricsEndpoint defines a scrapeable endpoint of a Kubernetes Pod serving Prometheus metrics.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| port | Name of the pod port this endpoint refers to. Mutually exclusive with targetPort. | string | false |
| targetPort | Deprecated: Use 'port' instead. | *intstr.IntOrString | false |
| path | HTTP path to scrape for metrics. | string | false |
| scheme | HTTP scheme to use for scraping. | string | false |
| params | Optional HTTP URL parameters | map[string][]string | false |
| interval | Interval at which metrics should be scraped | string | false |
| scrapeTimeout | Timeout after which the scrape is ended | string | false |
| honorLabels | HonorLabels chooses the metric's labels on collisions with target labels. | bool | false |
| honorTimestamps | HonorTimestamps controls whether Prometheus respects the timestamps present in scraped data. | *bool | false |
| metricRelabelConfigs | MetricRelabelConfigs to apply to samples before ingestion. | []*[RelabelConfig](#relabelconfig) | false |
| relabelConfigs | RelabelConfigs to apply to samples before ingestion. More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config | []*[RelabelConfig](#relabelconfig) | false |
| proxyURL | ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint. | *string | false |

[Back to TOC](#table-of-contents)

## PodMonitor

PodMonitor defines monitoring for a set of pods.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of desired Pod selection for target discovery by Prometheus. | [PodMonitorSpec](#podmonitorspec) | true |

[Back to TOC](#table-of-contents)

## PodMonitorList

PodMonitorList contains a list of PodMonitor

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][PodMonitor](#podmonitor) | true |

[Back to TOC](#table-of-contents)

## PodMonitorSpec

PodMonitorSpec contains specification parameters for a PodMonitor.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| jobLabel | The label to use to retrieve the job name from. | string | false |
| podTargetLabels | PodTargetLabels transfers labels on the Kubernetes Pod onto the target. | []string | false |
| podMetricsEndpoints | A list of endpoints allowed as part of this PodMonitor. | [][PodMetricsEndpoint](#podmetricsendpoint) | true |
| selector | Selector to select Pod objects. | [metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | true |
| namespaceSelector | Selector to select which namespaces the Endpoints objects are discovered from. | [NamespaceSelector](#namespaceselector) | false |
| sampleLimit | SampleLimit defines per-scrape limit on number of scraped samples that will be accepted. | uint64 | false |

[Back to TOC](#table-of-contents)

## QueueConfig

QueueConfig allows the tuning of remote_write queue_config parameters. This object is referenced in the RemoteWriteSpec object.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| capacity | Capacity is the number of samples to buffer per shard before we start dropping them. | int | false |
| minShards | MinShards is the minimum number of shards, i.e. amount of concurrency. | int | false |
| maxShards | MaxShards is the maximum number of shards, i.e. amount of concurrency. | int | false |
| maxSamplesPerSend | MaxSamplesPerSend is the maximum number of samples per send. | int | false |
| batchSendDeadline | BatchSendDeadline is the maximum time a sample will wait in buffer. | string | false |
| maxRetries | MaxRetries is the maximum number of times to retry a batch on recoverable errors. | int | false |
| minBackoff | MinBackoff is the initial retry delay. Gets doubled for every retry. | string | false |
| maxBackoff | MaxBackoff is the maximum retry delay. | string | false |

[Back to TOC](#table-of-contents)

## RelabelConfig

RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion. It defines `<metric_relabel_configs>`-section of Prometheus configuration. More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| sourceLabels | The source labels select values from existing labels. Their content is concatenated using the configured separator and matched against the configured regular expression for the replace, keep, and drop actions. | []string | false |
| separator | Separator placed between concatenated source label values. default is ';'. | string | false |
| targetLabel | Label to which the resulting value is written in a replace action. It is mandatory for replace actions. Regex capture groups are available. | string | false |
| regex | Regular expression against which the extracted value is matched. Default is '(.*)' | string | false |
| modulus | Modulus to take of the hash of the source label values. | uint64 | false |
| replacement | Replacement value against which a regex replace is performed if the regular expression matches. Regex capture groups are available. Default is '$1' | string | false |
| action | Action to perform based on regex matching. Default is 'replace' | string | false |

[Back to TOC](#table-of-contents)

## RemoteWriteSpec

RemoteWriteSpec defines the remote_write configuration for prometheus.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| url | The URL of the endpoint to send samples to. | string | true |
| name | The name of the remote write queue, must be unique if specified. The name is used in metrics and logging in order to differentiate queues. Only valid in Prometheus versions 2.15.0 and newer. | string | false |
| remoteTimeout | Timeout for requests to the remote write endpoint. | string | false |
| writeRelabelConfigs | The list of remote write relabel configurations. | [][RelabelConfig](#relabelconfig) | false |
| basicAuth | BasicAuth for the URL. | *[BasicAuth](#basicauth) | false |
| bearerToken | File to read bearer token for remote write. | string | false |
| bearerTokenFile | File to read bearer token for remote write. | string | false |
| tlsConfig | TLS Config to use for remote write. | *[TLSConfig](#tlsconfig) | false |
| proxyURL | Optional ProxyURL | string | false |
| queueConfig | QueueConfig allows tuning of the remote write queue parameters. | *[QueueConfig](#queueconfig) | false |

[Back to TOC](#table-of-contents)

## SecretOrConfigMap

SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| secret | Secret containing data to use for the targets. | *[v1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#secretkeyselector-v1-core) | false |
| configMap | ConfigMap containing data to use for the targets. | *v1.ConfigMapKeySelector | false |

[Back to TOC](#table-of-contents)

## ServiceMonitor

ServiceMonitor is the Schema for the servicemonitors API ServiceMonitor defines monitoring for a set of services.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec | Specification of desired Service selection for target discovery by Prometheus. | [ServiceMonitorSpec](#servicemonitorspec) | true |

[Back to TOC](#table-of-contents)

## ServiceMonitorList

ServiceMonitorList contains a list of ServiceMonitor

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][ServiceMonitor](#servicemonitor) | true |

[Back to TOC](#table-of-contents)

## ServiceMonitorSpec

ServiceMonitorSpec contains specification parameters for a ServiceMonitor.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| jobLabel | The label to use to retrieve the job name from. | string | false |
| targetLabels | TargetLabels transfers labels on the Kubernetes Service onto the target. | []string | false |
| podTargetLabels | PodTargetLabels transfers labels on the Kubernetes Pod onto the target. | []string | false |
| endpoints | A list of endpoints allowed as part of this ServiceMonitor. | [][Endpoint](#endpoint) | true |
| selector | Selector to select Endpoints objects. | [metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#labelselector-v1-meta) | true |
| namespaceSelector | Selector to select which namespaces the Endpoints objects are discovered from. | [NamespaceSelector](#namespaceselector) | false |
| sampleLimit | SampleLimit defines per-scrape limit on number of scraped samples that will be accepted. | uint64 | false |

[Back to TOC](#table-of-contents)

## TLSConfig

TLSConfig specifies TLS configuration parameters.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| caFile | Path to the CA cert in the Prometheus container to use for the targets. | string | false |
| ca | Stuct containing the CA cert to use for the targets. | [SecretOrConfigMap](#secretorconfigmap) | false |
| certFile | Path to the client cert file in the Prometheus container for the targets. | string | false |
| cert | Struct containing the client cert file for the targets. | [SecretOrConfigMap](#secretorconfigmap) | false |
| keyFile | Path to the client key file in the Prometheus container for the targets. | string | false |
| keySecret | Secret containing the client key file for the targets. | *[v1.SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#secretkeyselector-v1-core) | false |
| serverName | Used to verify the hostname for the targets. | string | false |
| insecureSkipVerify | Disable target certificate validation. | bool | false |

[Back to TOC](#table-of-contents)
