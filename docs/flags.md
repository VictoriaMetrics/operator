```
Usage of bin/operator:
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
