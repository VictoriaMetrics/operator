---
weight: 4
title: Configuration
menu:
  docs:
    parent: "operator"
    weight: 4
aliases:
  - /operator/configuration/
  - /operator/configuration/index.html
---
Operator configured by env variables and command-line flags, list of env variables can be found
on [Variables](https://docs.victoriametrics.com/operator/vars/) page.

It defines default configuration options, like images for components, timeouts, features.

In addition, the operator has a special startup mode for outputting all variables, their types and default values.
For instance, with this mode you can know versions of VM components, which are used by default: 

```sh
./operator --printDefaults

# This application is configured via the environment. The following environment variables can be used:
# 
# KEY                                                          TYPE                              DEFAULT                                                           REQUIRED    DESCRIPTION
# VM_USECUSTOMCONFIGRELOADER                                   True or False                     false                                                                                                                                                                                   
# VM_CUSTOMCONFIGRELOADERIMAGE                                 String                            victoriametrics/operator:config-reloader-v0.32.0                                                                                                       
# VM_VMALERTDEFAULT_IMAGE                                      String                            victoriametrics/vmalert                                                       
# VM_VMALERTDEFAULT_VERSION                                    String                            v1.93.3                                                                                                                                                 
# VM_VMALERTDEFAULT_USEDEFAULTRESOURCES                        True or False                     true                                                                          
# VM_VMALERTDEFAULT_RESOURCE_LIMIT_MEM                         String                            500Mi                                                                         
# VM_VMALERTDEFAULT_RESOURCE_LIMIT_CPU                         String                            200m                                                                                                                                                                                                                            
# ...
```

You can choose output format for variables with `--printFormat` flag, possible values: `json`, `yaml`, `list` and `table` (default):

```sh
.operator --printDefaults --printFormat=json

# {
#     'VM_USECUSTOMCONFIGRELOADER': 'false',
#     'VM_CUSTOMCONFIGRELOADERIMAGE': 'victoriametrics/operator:config-reloader-v0.32.0',
#     'VM_VMALERTDEFAULT_IMAGE': 'victoriametrics/vmalert',
#     'VM_VMALERTDEFAULT_VERSION': 'v1.93.3',
# ...
#     'VM_FORCERESYNCINTERVAL': '60s',
#     'VM_ENABLESTRICTSECURITY': 'true'
# }
```

## List of command-line flags

Pass `-help` to operator binary in order to see the list of supported command-line flags with their description:

```shellhelp
Usage of ./bin/operator:
  -client.burst int
        defines K8s client burst (default 100)
  -client.qps int
        defines K8s client QPS. The value should be increased for the cluster with large number of objects > 10_000. (default 50)
  -controller.cacheSyncTimeout duration
        controls timeout for caches to be synced. (default 3m0s)
  -controller.disableCRDOwnership
        disables CRD ownership add to cluster wide objects, must be disabled for clusters, lower than v1.16.0
  -controller.disableCacheFor string
        disables client for cache for API resources. Supported objects - namespace,pod,service,secret,configmap,deployment,statefulset (default "configmap,secret")
  -controller.disableReconcileFor string
        disables reconcile controllers for given list of comma separated CRD names. For example - VMCluster,VMSingle,VMAuth.Note, child controllers still require parent object CRDs.
  -controller.maxConcurrentReconciles int
        Configures number of concurrent reconciles. It should improve performance for clusters with many objects. (default 15)
  -controller.prometheusCRD.resyncPeriod duration
        Configures resync period for prometheus CRD converter. Disabled by default
  -controller.statusLastUpdateTimeTTL duration
        Configures TTL for LastUpdateTime status.conditions fields. It's used to detect stale parent objects on child objects. Like VMAlert->VMRule .status.Conditions.Type (default 1h0m0s)
  -default.kubernetesVersion.major uint
        Major version of kubernetes server, if operator cannot parse actual kubernetes response (default 1)
  -default.kubernetesVersion.minor uint
        Minor version of kubernetes server, if operator cannot parse actual kubernetes response (default 21)
  -disableSecretKeySpaceTrim
        disables trim of space at Secret/Configmap value content. It's a common mistake to put new line to the base64 encoded secret value.
  -health-probe-bind-address string
        The address the probes (health, ready) binds to. (default ":8081")
  -leader-elect
        Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.
  -leader-elect-id string
        Defines the name of the resource that leader election will use for holding the leader lock. (default "57410f0d.victoriametrics.com")
  -leader-elect-namespace string
        Defines optional namespace name in which the leader election resource will be created. By default, uses in-cluster namespace name.
  -loggerJSONFields string
        Allows renaming fields in JSON formatted logsExample: "ts:timestamp,msg:message" renames "ts" to "timestamp" and "msg" to "message".Supported fields: ts, level, caller, msg
  -metrics-bind-address string
        The address the metric endpoint binds to. (default ":8080")
  -mtls.CAName string
        Optional name of TLS Root CA for verifying client certificates at the corresponding -metrics-bind-address when -mtls.enable is enabled. By default the host system TLS Root CA is used for client certificate verification.  (default "clietCA.crt")
  -mtls.enable
        Whether to require valid client certificate for https requests to the corresponding -metrics-bind-address. This flag works only if -tls.enable flag is set.
  -pprof-addr string
        The address for pprof/debug API. Empty value disables server (default ":8435")
  -printDefaults
        print all variables with their default values and exit
  -printFormat string
        output format for --printDefaults. Can be table, json, yaml or list (default "table")
  -tls.certDir string
        root directory for metrics webserver cert, key and mTLS CA. (default "/tmp/k8s-metrics-server/serving-certs")
  -tls.certName string
        name of metric server Tls certificate inside tls.certDir. Default -  (default "tls.crt")
  -tls.enable
        enables secure tls (https) for metrics webserver.
  -tls.keyName string
        name of metric server Tls key inside tls.certDir. Default - tls.key (default "tls.key")
  -version
        Show operator version
  -webhook.certDir string
        root directory for webhook cert and key (default "/tmp/k8s-webhook-server/serving-certs/")
  -webhook.certName string
        name of webhook server Tls certificate inside tls.certDir (default "tls.crt")
  -webhook.enable
        adds webhook server, you must mount cert and key or use cert-manager
  -webhook.keyName string
        name of webhook server Tls key inside tls.certDir (default "tls.key")
  -webhook.port int
        port to start webhook server on (default 9443)
  -zap-devel
        Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn). Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error)
  -zap-encoder value
        Zap log encoding (one of 'json' or 'console')
  -zap-log-level value
        Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', or any integer value > 0 which corresponds to custom debug levels of increasing verbosity
  -zap-stacktrace-level value
        Zap Level at and above which stacktraces are captured (one of 'info', 'error', 'panic').
  -zap-time-encoding value
        Zap time encoding (one of 'epoch', 'millis', 'nano', 'iso8601', 'rfc3339' or 'rfc3339nano'). Defaults to 'epoch'.
```

## Conversion of prometheus-operator objects

You can read detailed instructions about configuring prometheus-objects conversion in [this document](https://docs.victoriametrics.com/operator/migration/).

## Helm-charts

In [Helm charts](https://docs.victoriametrics.com/helm) some important configuration parameters are implemented as separate flags in `values.yaml`:

### victoria-metrics-k8s-stack

For possible values refer to [parameters](https://docs.victoriametrics.com/helm/victoriametrics-k8s-stack#parameters).

Also, checkout [here possible ENV variables](https://docs.victoriametrics.com/operator/vars/) to configure operator behaviour.
ENV variables can be set in the `victoria-metrics-operator.env` section.

```yaml
# values.yaml

victoria-metrics-operator:
  image:
    # -- Image repository
    repository: victoriametrics/operator
    # -- Image tag
    tag: v0.35.0
    # -- Image pull policy
    pullPolicy: IfNotPresent

  # -- Tells helm to remove CRD after chart remove
  cleanupCRD: true
  cleanupImage:
    repository: gcr.io/google_containers/hyperkube
    tag: v1.18.0
    pullPolicy: IfNotPresent

  operator:
    # -- By default, operator converts prometheus-operator objects.
    disable_prometheus_converter: false
    # -- Compare-options and sync-options for prometheus objects converted by operator for properly use with ArgoCD
    prometheus_converter_add_argocd_ignore_annotations: false
    # -- Enables ownership reference for converted prometheus-operator objects,
    # it will remove corresponding victoria-metrics objects in case of deletion prometheus one.
    enable_converter_ownership: false
    # -- By default, operator creates psp for its objects.
    psp_auto_creation_enabled: true
    # -- Enables custom config-reloader, bundled with operator.
    # It should reduce  vmagent and vmauth config sync-time and make it predictable.
    useCustomConfigReloader: false

  # -- extra settings for the operator deployment. full list Ref: https://docs.victoriametrics.com/operator/vars
  env:
    # -- default version for vmsingle
    - name: VM_VMSINGLEDEFAULT_VERSION
      value: v1.43.0
    # -- container registry name prefix, e.g. docker.io
    - name: VM_CONTAINERREGISTRY
      value: ""
    # -- image for custom reloader (see the useCustomConfigReloader parameter)
    - name: VM_CUSTOMCONFIGRELOADERIMAGE
      value: victoriametrics/operator:config-reloader-v0.32.0

  # By default, the operator will watch all the namespaces
  # If you want to override this behavior, specify the namespace it needs to watch separated by a comma.
  # Ex: my_namespace1,my_namespace2
  watchNamespace: ""

  # Count of operator instances (can be increased for HA mode)
  replicaCount: 1

  # -- VM operator log level
  # -- possible values: info and error.
  logLevel: "info"

  # -- Resource object
  resources:
    {}
    # limits:
    #   cpu: 120m
    #   memory: 320Mi
    # requests:
    #   cpu: 80m
    #   memory: 120Mi
```

### victoria-metrics-operator

For possible values refer to [parameters](https://docs.victoriametrics.com/helm/victoriametrics-operator#parameters).

Also, checkout [here possible ENV variables](https://docs.victoriametrics.com/operator/vars/) to configure operator behaviour.
ENV variables can be set in the `env` section.

```yaml
# values.yaml

image:
  # -- Image repository
  repository: victoriametrics/operator
  # -- Image tag
  tag: v0.35.0
  # -- Image pull policy
  pullPolicy: IfNotPresent

operator:
  # -- By default, operator converts prometheus-operator objects.
  disable_prometheus_converter: false
  # -- Compare-options and sync-options for prometheus objects converted by operator for properly use with ArgoCD
  prometheus_converter_add_argocd_ignore_annotations: false
  # -- Enables ownership reference for converted prometheus-operator objects,
  # it will remove corresponding victoria-metrics objects in case of deletion prometheus one.
  enable_converter_ownership: false
  # -- By default, operator creates psp for its objects.
  psp_auto_creation_enabled: true
  # -- Enables custom config-reloader, bundled with operator.
  # It should reduce  vmagent and vmauth config sync-time and make it predictable.
  useCustomConfigReloader: false

# -- extra settings for the operator deployment. full list Ref: https://docs.victoriametrics.com/operator/vars
env:
  # -- default version for vmsingle
  - name: VM_VMSINGLEDEFAULT_VERSION
    value: v1.43.0
  # -- container registry name prefix, e.g. docker.io
  - name: VM_CONTAINERREGISTRY
    value: ""
  # -- image for custom reloader (see the useCustomConfigReloader parameter)
  - name: VM_CUSTOMCONFIGRELOADERIMAGE
    value: victoriametrics/operator:config-reloader-v0.32.0

# By default, the operator will watch all the namespaces
# If you want to override this behavior, specify the namespace it needs to watch separated by a comma.
# Ex: my_namespace1,my_namespace2
watchNamespace: ""

# Count of operator instances (can be increased for HA mode)
replicaCount: 1

# -- VM operator log level
# -- possible values: info and error.
logLevel: "info"

# -- Resource object
resources:
  {}
  # limits:
  #   cpu: 120m
  #   memory: 320Mi
  # requests:
  #   cpu: 80m
  #   memory: 120Mi
```

## Namespaced mode

By default, the operator will watch all namespaces, but it can be configured to watch only specific namespace or multiple namespaces.

If you want to override this behavior, specify the namespace:

- in the `WATCH_NAMESPACE` environment variable.
- in the `watchNamespace` field in the `values.yaml` file of helm-charts.

The operator supports comma separated namespace names for this setting.

If namespaced mode is enabled, operator uses a limited set of features:
- it cannot make any cluster wide API calls.
- it cannot assign rbac permissions for `vmagent`. It must be done manually via serviceAccount for vmagent.
- it ignores namespaceSelector fields at CRD objects and uses `WATCH_NAMESPACE` value for object matching.

At each namespace operator must have a set of required permissions, an example can be found at [this file](https://github.com/VictoriaMetrics/operator/blob/master/config/examples/operator_rbac_for_single_namespace.yaml).

## Monitoring of cluster components

By default, operator creates [VMServiceScrape](https://docs.victoriametrics.com/operator/resources/vmservicescrape/) 
object for each component that it manages.

You can disable this behaviour with `VM_DISABLESELFSERVICESCRAPECREATION` environment variable:

```shell
VM_DISABLESELFSERVICESCRAPECREATION=false
```

Also, you can override default configuration for self-scraping with `ServiceScrapeSpec` field in each deployable resource 
(`vmcluster/select`, `vmcluster/insert`, `vmcluster/storage`, `vmagent`, `vmalert`, `vmalertmanager`, `vmauth`, `vmsingle`):

## CRD Validation

Operator supports validation admission webhook [docs](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)

It checks resources configuration and returns errors to caller before resource will be created at kubernetes api.
This should reduce errors and simplify debugging.

Validation hooks at operator side must be enabled with flags:

```sh
./operator
    --webhook.enable
    # optional configuration for certDir and tls names.
    --webhook.certDir=/tmp/k8s-webhook-server/serving-certs/
    --webhook.keyName=tls.key
    --webhook.certName=tls.crt
```

You have to mount correct certificates at give directory.
It can be simplified with cert-manager and kustomize command:

```sh
kustomize build config/deployments/webhook/
```

### Requirements

- Valid certificate with key must be provided to operator
- Valid CABundle must be added to the `ValidatingWebhookConfiguration`

### Useful links

- [k8s admission webhooks](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)
