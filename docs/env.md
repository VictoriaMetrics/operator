
| variable name | variable default value | variable required | variable description |
| --- | --- | --- | --- |
| VM_USECUSTOMCONFIGRELOADER | false | false | enables custom config reloader for vmauth and vmagent, it should speed-up config reloading process. |
| VM_CONTAINERREGISTRY | - | false | container registry name prefix, e.g. docker.io |
| VM_CUSTOMCONFIGRELOADERIMAGE | victoriametrics/operator:config-reloader-v0.56.0 | false |  |
| VM_PSPAUTOCREATEENABLED | false | false |  |
| VM_CONFIG_RELOADER_LIMIT_CPU | unlimited | false | defines global resource.limits.cpu for all config-reloader containers |
| VM_CONFIG_RELOADER_LIMIT_MEMORY | unlimited | false | defines global resource.limits.memory for all config-reloader containers |
| VM_CONFIG_RELOADER_REQUEST_CPU | - | false | defines global resource.requests.cpu for all config-reloader containers |
| VM_CONFIG_RELOADER_REQUEST_MEMORY | - | false | defines global resource.requests.memory for all config-reloader containers |
| VM_VLOGSDEFAULT_IMAGE | victoriametrics/victoria-logs | false |  |
| VM_VLOGSDEFAULT_VERSION | ${VM_LOGS_VERSION}-victorialogs | false |  |
| VM_VLOGSDEFAULT_PORT | 9428 | false |  |
| VM_VLOGSDEFAULT_USEDEFAULTRESOURCES | true | false |  |
| VM_VLOGSDEFAULT_RESOURCE_LIMIT_MEM | 1500Mi | false |  |
| VM_VLOGSDEFAULT_RESOURCE_LIMIT_CPU | 1200m | false |  |
| VM_VLOGSDEFAULT_RESOURCE_REQUEST_MEM | 500Mi | false |  |
| VM_VLOGSDEFAULT_RESOURCE_REQUEST_CPU | 150m | false |  |
| VM_VMALERTDEFAULT_IMAGE | victoriametrics/vmalert | false |  |
| VM_VMALERTDEFAULT_VERSION | ${VM_METRICS_VERSION} | false |  |
| VM_VMALERTDEFAULT_CONFIGRELOADIMAGE | jimmidyson/configmap-reload:v0.3.0 | false |  |
| VM_VMALERTDEFAULT_PORT | 8080 | false |  |
| VM_VMALERTDEFAULT_USEDEFAULTRESOURCES | true | false |  |
| VM_VMALERTDEFAULT_RESOURCE_LIMIT_MEM | 500Mi | false |  |
| VM_VMALERTDEFAULT_RESOURCE_LIMIT_CPU | 200m | false |  |
| VM_VMALERTDEFAULT_RESOURCE_REQUEST_MEM | 200Mi | false |  |
| VM_VMALERTDEFAULT_RESOURCE_REQUEST_CPU | 50m | false |  |
| VM_VMALERTDEFAULT_CONFIGRELOADERCPU | 10m | false | deprecated use VM_CONFIG_RELOADER_REQUEST_CPU instead |
| VM_VMALERTDEFAULT_CONFIGRELOADERMEMORY | 25Mi | false | deprecated use VM_CONFIG_RELOADER_REQUEST_MEMORY instead |
| VM_VMSERVICESCRAPEDEFAULT_ENFORCEENDPOINTSLICES | false | false | Use endpointslices instead of endpoints as discovery role for vmservicescrape when generate scrape config for vmagent. |
| VM_VMAGENTDEFAULT_IMAGE | victoriametrics/vmagent | false |  |
| VM_VMAGENTDEFAULT_VERSION | ${VM_METRICS_VERSION} | false |  |
| VM_VMAGENTDEFAULT_CONFIGRELOADIMAGE | quay.io/prometheus-operator/prometheus-config-reloader:v0.68.0 | false |  |
| VM_VMAGENTDEFAULT_PORT | 8429 | false |  |
| VM_VMAGENTDEFAULT_USEDEFAULTRESOURCES | true | false |  |
| VM_VMAGENTDEFAULT_RESOURCE_LIMIT_MEM | 500Mi | false |  |
| VM_VMAGENTDEFAULT_RESOURCE_LIMIT_CPU | 200m | false |  |
| VM_VMAGENTDEFAULT_RESOURCE_REQUEST_MEM | 200Mi | false |  |
| VM_VMAGENTDEFAULT_RESOURCE_REQUEST_CPU | 50m | false |  |
| VM_VMAGENTDEFAULT_CONFIGRELOADERCPU | 10m | false | deprecated use VM_CONFIG_RELOADER_REQUEST_CPU instead |
| VM_VMAGENTDEFAULT_CONFIGRELOADERMEMORY | 25Mi | false | deprecated use VM_CONFIG_RELOADER_REQUEST_MEMORY instead |
| VM_VMSINGLEDEFAULT_IMAGE | victoriametrics/victoria-metrics | false |  |
| VM_VMSINGLEDEFAULT_VERSION | ${VM_METRICS_VERSION} | false |  |
| VM_VMSINGLEDEFAULT_PORT | 8429 | false |  |
| VM_VMSINGLEDEFAULT_USEDEFAULTRESOURCES | true | false |  |
| VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM | 1500Mi | false |  |
| VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU | 1200m | false |  |
| VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_MEM | 500Mi | false |  |
| VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_CPU | 150m | false |  |
| VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES | true | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_IMAGE | victoriametrics/vmselect | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_VERSION | ${VM_METRICS_VERSION}-cluster | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_PORT | 8481 | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_MEM | 1000Mi | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_CPU | 500m | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_MEM | 500Mi | false |  |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_CPU | 100m | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_IMAGE | victoriametrics/vmstorage | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_VERSION | ${VM_METRICS_VERSION}-cluster | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_VMINSERTPORT | 8400 | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_VMSELECTPORT | 8401 | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_PORT | 8482 | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_MEM | 1500Mi | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_CPU | 1000m | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_MEM | 500Mi | false |  |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_CPU | 250m | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_IMAGE | victoriametrics/vminsert | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_VERSION | ${VM_METRICS_VERSION}-cluster | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_PORT | 8480 | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_MEM | 500Mi | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_CPU | 500m | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_MEM | 200Mi | false |  |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_CPU | 150m | false |  |
| VM_VMALERTMANAGER_CONFIGRELOADERIMAGE | jimmidyson/configmap-reload:v0.3.0 | false |  |
| VM_VMALERTMANAGER_CONFIGRELOADERCPU | 10m | false | deprecated use VM_CONFIG_RELOADER_REQUEST_CPU instead |
| VM_VMALERTMANAGER_CONFIGRELOADERMEMORY | 25Mi | false | deprecated use VM_CONFIG_RELOADER_REQUEST_MEMORY instead |
| VM_VMALERTMANAGER_ALERTMANAGERDEFAULTBASEIMAGE | prom/alertmanager | false |  |
| VM_VMALERTMANAGER_ALERTMANAGERVERSION | v0.27.0 | false |  |
| VM_VMALERTMANAGER_LOCALHOST | 127.0.0.1 | false |  |
| VM_VMALERTMANAGER_USEDEFAULTRESOURCES | true | false |  |
| VM_VMALERTMANAGER_RESOURCE_LIMIT_MEM | 256Mi | false |  |
| VM_VMALERTMANAGER_RESOURCE_LIMIT_CPU | 100m | false |  |
| VM_VMALERTMANAGER_RESOURCE_REQUEST_MEM | 56Mi | false |  |
| VM_VMALERTMANAGER_RESOURCE_REQUEST_CPU | 30m | false |  |
| VM_DISABLESELFSERVICESCRAPECREATION | false | false |  |
| VM_VMBACKUP_IMAGE | victoriametrics/vmbackupmanager | false |  |
| VM_VMBACKUP_VERSION | ${VM_METRICS_VERSION}-enterprise | false |  |
| VM_VMBACKUP_PORT | 8300 | false |  |
| VM_VMBACKUP_USEDEFAULTRESOURCES | true | false |  |
| VM_VMBACKUP_RESOURCE_LIMIT_MEM | 500Mi | false |  |
| VM_VMBACKUP_RESOURCE_LIMIT_CPU | 500m | false |  |
| VM_VMBACKUP_RESOURCE_REQUEST_MEM | 200Mi | false |  |
| VM_VMBACKUP_RESOURCE_REQUEST_CPU | 150m | false |  |
| VM_VMAUTHDEFAULT_IMAGE | victoriametrics/vmauth | false |  |
| VM_VMAUTHDEFAULT_VERSION | ${VM_METRICS_VERSION} | false |  |
| VM_VMAUTHDEFAULT_CONFIGRELOADIMAGE | quay.io/prometheus-operator/prometheus-config-reloader:v0.68.0 | false |  |
| VM_VMAUTHDEFAULT_PORT | 8427 | false |  |
| VM_VMAUTHDEFAULT_USEDEFAULTRESOURCES | true | false |  |
| VM_VMAUTHDEFAULT_RESOURCE_LIMIT_MEM | 300Mi | false |  |
| VM_VMAUTHDEFAULT_RESOURCE_LIMIT_CPU | 200m | false |  |
| VM_VMAUTHDEFAULT_RESOURCE_REQUEST_MEM | 100Mi | false |  |
| VM_VMAUTHDEFAULT_RESOURCE_REQUEST_CPU | 50m | false |  |
| VM_VMAUTHDEFAULT_CONFIGRELOADERCPU | 10m | false | deprecated use VM_CONFIG_RELOADER_REQUEST_CPU instead |
| VM_VMAUTHDEFAULT_CONFIGRELOADERMEMORY | 25Mi | false | deprecated use VM_CONFIG_RELOADER_REQUEST_MEMORY instead |
| VM_ENABLEDPROMETHEUSCONVERTER_PODMONITOR | true | false |  |
| VM_ENABLEDPROMETHEUSCONVERTER_SERVICESCRAPE | true | false |  |
| VM_ENABLEDPROMETHEUSCONVERTER_PROMETHEUSRULE | true | false |  |
| VM_ENABLEDPROMETHEUSCONVERTER_PROBE | true | false |  |
| VM_ENABLEDPROMETHEUSCONVERTER_ALERTMANAGERCONFIG | true | false |  |
| VM_ENABLEDPROMETHEUSCONVERTER_SCRAPECONFIG | true | false |  |
| VM_FILTERCHILDLABELPREFIXES | - | false |  |
| VM_FILTERCHILDANNOTATIONPREFIXES | - | false |  |
| VM_PROMETHEUSCONVERTERADDARGOCDIGNOREANNOTATIONS | false | false | adds compare-options and sync-options for prometheus objects converted by operator. It helps to properly use converter with ArgoCD |
| VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES | false | false |  |
| VM_FILTERPROMETHEUSCONVERTERLABELPREFIXES | - | false | allows filtering for converted labels, labels with matched prefix will be ignored |
| VM_FILTERPROMETHEUSCONVERTERANNOTATIONPREFIXES | - | false | allows filtering for converted annotations, annotations with matched prefix will be ignored |
| VM_CLUSTERDOMAINNAME | - | false | Defines domain name suffix for in-cluster addresses most known ClusterDomainName is .cluster.local |
| VM_APPREADYTIMEOUT | 80s | false | Defines deadline for deployment/statefulset to transit into ready state to wait for transition to ready state |
| VM_PODWAITREADYTIMEOUT | 80s | false | Defines single pod deadline to wait for transition to ready state |
| VM_PODWAITREADYINTERVALCHECK | 5s | false | Defines poll interval for pods ready check at statefulset rollout update |
| VM_FORCERESYNCINTERVAL | 60s | false | configures force resync interval for VMAgent, VMAlert, VMAlertmanager and VMAuth. |
| VM_ENABLESTRICTSECURITY | false | false | EnableStrictSecurity will add default `securityContext` to pods and containers created by operator Default PodSecurityContext include: 1. RunAsNonRoot: true 2. RunAsUser/RunAsGroup/FSGroup: 65534 '65534' refers to 'nobody' in all the used default images like alpine, busybox. If you're using customize image, please make sure '65534' is a valid uid in there or specify SecurityContext. 3. FSGroupChangePolicy: &onRootMismatch If KubeVersion>=1.20, use `FSGroupChangePolicy="onRootMismatch"` to skip the recursive permission change when the root of the volume already has the correct permissions 4. SeccompProfile:      type: RuntimeDefault Use `RuntimeDefault` seccomp profile by default, which is defined by the container runtime, instead of using the Unconfined (seccomp disabled) mode. Default container SecurityContext include: 1. AllowPrivilegeEscalation: false 2. ReadOnlyRootFilesystem: true 3. Capabilities:      drop:        - all turn off `EnableStrictSecurity` by default, see https://github.com/VictoriaMetrics/operator/issues/749 for details |
