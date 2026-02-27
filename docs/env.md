| Environment variables |
| --- |
| VM_METRICS_VERSION: `v1.136.0` <a href="#variables-vm-metrics-version" id="variables-vm-metrics-version">#</a> |
| VM_LOGS_VERSION: `v1.47.0` <a href="#variables-vm-logs-version" id="variables-vm-logs-version">#</a> |
| VM_ANOMALY_VERSION: `v1.28.5` <a href="#variables-vm-anomaly-version" id="variables-vm-anomaly-version">#</a> |
| VM_TRACES_VERSION: `v0.7.0` <a href="#variables-vm-traces-version" id="variables-vm-traces-version">#</a> |
| VM_OPERATOR_VERSION: `v0.66.1` <a href="#variables-vm-operator-version" id="variables-vm-operator-version">#</a> |
| VM_GATEWAY_API_ENABLED: `false` <a href="#variables-vm-gateway-api-enabled" id="variables-vm-gateway-api-enabled">#</a> |
| VM_VPA_API_ENABLED: `false` <a href="#variables-vm-vpa-api-enabled" id="variables-vm-vpa-api-enabled">#</a> |
| WATCH_NAMESPACE: `-` <a href="#variables-watch-namespace" id="variables-watch-namespace">#</a><br>Defines a list of namespaces to be watched by operator. Operator don't perform any cluster wide API calls if namespaces not empty. In case of empty list it performs only clusterwide api calls. |
| EXCLUDE_NAMESPACE: `-` <a href="#variables-exclude-namespace" id="variables-exclude-namespace">#</a><br>Defines a list of namespaces to exclude from being watched by operator. |
| VM_CONTAINERREGISTRY: `-` <a href="#variables-vm-containerregistry" id="variables-vm-containerregistry">#</a><br>container registry name prefix, e.g. docker.io |
| VM_CUSTOMCONFIGRELOADERIMAGE: `-` <a href="#variables-vm-customconfigreloaderimage" id="variables-vm-customconfigreloaderimage">#</a><br>Deprecated: use VM_CONFIG_RELOADER_IMAGE instead |
| VM_PSPAUTOCREATEENABLED: `false` <a href="#variables-vm-pspautocreateenabled" id="variables-vm-pspautocreateenabled">#</a> |
| VM_ENABLETCP6: `false` <a href="#variables-vm-enabletcp6" id="variables-vm-enabletcp6">#</a> |
| VM_CONFIG_RELOADER_IMAGE: `victoriametrics/operator:config-reloader-${VM_OPERATOR_VERSION}` <a href="#variables-vm-config-reloader-image" id="variables-vm-config-reloader-image">#</a><br>default image for all config-reloader containers |
| VM_CONFIG_RELOADER_LIMIT_MEMORY: `unlimited` <a href="#variables-vm-config-reloader-limit-memory" id="variables-vm-config-reloader-limit-memory">#</a><br>defines global resource.limits.memory for all config-reloader containers |
| VM_CONFIG_RELOADER_LIMIT_CPU: `unlimited` <a href="#variables-vm-config-reloader-limit-cpu" id="variables-vm-config-reloader-limit-cpu">#</a><br>defines global resource.limits.cpu for all config-reloader containers |
| VM_CONFIG_RELOADER_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-config-reloader-limit-ephemeral-storage" id="variables-vm-config-reloader-limit-ephemeral-storage">#</a><br>defines global resource.limits.ephemeral-storage for all config-reloader containers |
| VM_CONFIG_RELOADER_REQUEST_MEMORY: `25Mi` <a href="#variables-vm-config-reloader-request-memory" id="variables-vm-config-reloader-request-memory">#</a><br>defines global resource.requests.memory for all config-reloader containers |
| VM_CONFIG_RELOADER_REQUEST_CPU: `10m` <a href="#variables-vm-config-reloader-request-cpu" id="variables-vm-config-reloader-request-cpu">#</a><br>defines global resource.requests.cpu for all config-reloader containers |
| VM_CONFIG_RELOADER_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-config-reloader-request-ephemeral-storage" id="variables-vm-config-reloader-request-ephemeral-storage">#</a><br>defines global resource.requests.ephemeral-storage for all config-reloader containers |
| VM_VLOGSDEFAULT_IMAGE: `victoriametrics/victoria-logs` <a href="#variables-vm-vlogsdefault-image" id="variables-vm-vlogsdefault-image">#</a> |
| VM_VLOGSDEFAULT_VERSION: `${VM_LOGS_VERSION}` <a href="#variables-vm-vlogsdefault-version" id="variables-vm-vlogsdefault-version">#</a> |
| VM_VLOGSDEFAULT_PORT: `9428` <a href="#variables-vm-vlogsdefault-port" id="variables-vm-vlogsdefault-port">#</a> |
| VM_VLOGSDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vlogsdefault-usedefaultresources" id="variables-vm-vlogsdefault-usedefaultresources">#</a> |
| VM_VLOGSDEFAULT_RESOURCE_LIMIT_MEM: `1500Mi` <a href="#variables-vm-vlogsdefault-resource-limit-mem" id="variables-vm-vlogsdefault-resource-limit-mem">#</a> |
| VM_VLOGSDEFAULT_RESOURCE_LIMIT_CPU: `1200m` <a href="#variables-vm-vlogsdefault-resource-limit-cpu" id="variables-vm-vlogsdefault-resource-limit-cpu">#</a> |
| VM_VLOGSDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlogsdefault-resource-limit-ephemeral-storage" id="variables-vm-vlogsdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VLOGSDEFAULT_RESOURCE_REQUEST_MEM: `500Mi` <a href="#variables-vm-vlogsdefault-resource-request-mem" id="variables-vm-vlogsdefault-resource-request-mem">#</a> |
| VM_VLOGSDEFAULT_RESOURCE_REQUEST_CPU: `150m` <a href="#variables-vm-vlogsdefault-resource-request-cpu" id="variables-vm-vlogsdefault-resource-request-cpu">#</a> |
| VM_VLOGSDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlogsdefault-resource-request-ephemeral-storage" id="variables-vm-vlogsdefault-resource-request-ephemeral-storage">#</a> |
| VM_VLAGENTDEFAULT_IMAGE: `victoriametrics/vlagent` <a href="#variables-vm-vlagentdefault-image" id="variables-vm-vlagentdefault-image">#</a> |
| VM_VLAGENTDEFAULT_VERSION: `${VM_LOGS_VERSION}` <a href="#variables-vm-vlagentdefault-version" id="variables-vm-vlagentdefault-version">#</a> |
| VM_VLAGENTDEFAULT_PORT: `9429` <a href="#variables-vm-vlagentdefault-port" id="variables-vm-vlagentdefault-port">#</a> |
| VM_VLAGENTDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vlagentdefault-usedefaultresources" id="variables-vm-vlagentdefault-usedefaultresources">#</a> |
| VM_VLAGENTDEFAULT_RESOURCE_LIMIT_MEM: `500Mi` <a href="#variables-vm-vlagentdefault-resource-limit-mem" id="variables-vm-vlagentdefault-resource-limit-mem">#</a> |
| VM_VLAGENTDEFAULT_RESOURCE_LIMIT_CPU: `200m` <a href="#variables-vm-vlagentdefault-resource-limit-cpu" id="variables-vm-vlagentdefault-resource-limit-cpu">#</a> |
| VM_VLAGENTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlagentdefault-resource-limit-ephemeral-storage" id="variables-vm-vlagentdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VLAGENTDEFAULT_RESOURCE_REQUEST_MEM: `200Mi` <a href="#variables-vm-vlagentdefault-resource-request-mem" id="variables-vm-vlagentdefault-resource-request-mem">#</a> |
| VM_VLAGENTDEFAULT_RESOURCE_REQUEST_CPU: `50m` <a href="#variables-vm-vlagentdefault-resource-request-cpu" id="variables-vm-vlagentdefault-resource-request-cpu">#</a> |
| VM_VLAGENTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlagentdefault-resource-request-ephemeral-storage" id="variables-vm-vlagentdefault-resource-request-ephemeral-storage">#</a> |
| VM_VLSINGLEDEFAULT_IMAGE: `victoriametrics/victoria-logs` <a href="#variables-vm-vlsingledefault-image" id="variables-vm-vlsingledefault-image">#</a> |
| VM_VLSINGLEDEFAULT_VERSION: `${VM_LOGS_VERSION}` <a href="#variables-vm-vlsingledefault-version" id="variables-vm-vlsingledefault-version">#</a> |
| VM_VLSINGLEDEFAULT_PORT: `9428` <a href="#variables-vm-vlsingledefault-port" id="variables-vm-vlsingledefault-port">#</a> |
| VM_VLSINGLEDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vlsingledefault-usedefaultresources" id="variables-vm-vlsingledefault-usedefaultresources">#</a> |
| VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_MEM: `1500Mi` <a href="#variables-vm-vlsingledefault-resource-limit-mem" id="variables-vm-vlsingledefault-resource-limit-mem">#</a> |
| VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_CPU: `1200m` <a href="#variables-vm-vlsingledefault-resource-limit-cpu" id="variables-vm-vlsingledefault-resource-limit-cpu">#</a> |
| VM_VLSINGLEDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlsingledefault-resource-limit-ephemeral-storage" id="variables-vm-vlsingledefault-resource-limit-ephemeral-storage">#</a> |
| VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_MEM: `500Mi` <a href="#variables-vm-vlsingledefault-resource-request-mem" id="variables-vm-vlsingledefault-resource-request-mem">#</a> |
| VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_CPU: `150m` <a href="#variables-vm-vlsingledefault-resource-request-cpu" id="variables-vm-vlsingledefault-resource-request-cpu">#</a> |
| VM_VLSINGLEDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlsingledefault-resource-request-ephemeral-storage" id="variables-vm-vlsingledefault-resource-request-ephemeral-storage">#</a> |
| VM_VTSINGLEDEFAULT_IMAGE: `victoriametrics/victoria-traces` <a href="#variables-vm-vtsingledefault-image" id="variables-vm-vtsingledefault-image">#</a> |
| VM_VTSINGLEDEFAULT_VERSION: `${VM_TRACES_VERSION}` <a href="#variables-vm-vtsingledefault-version" id="variables-vm-vtsingledefault-version">#</a> |
| VM_VTSINGLEDEFAULT_PORT: `10428` <a href="#variables-vm-vtsingledefault-port" id="variables-vm-vtsingledefault-port">#</a> |
| VM_VTSINGLEDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vtsingledefault-usedefaultresources" id="variables-vm-vtsingledefault-usedefaultresources">#</a> |
| VM_VTSINGLEDEFAULT_RESOURCE_LIMIT_MEM: `1500Mi` <a href="#variables-vm-vtsingledefault-resource-limit-mem" id="variables-vm-vtsingledefault-resource-limit-mem">#</a> |
| VM_VTSINGLEDEFAULT_RESOURCE_LIMIT_CPU: `1200m` <a href="#variables-vm-vtsingledefault-resource-limit-cpu" id="variables-vm-vtsingledefault-resource-limit-cpu">#</a> |
| VM_VTSINGLEDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtsingledefault-resource-limit-ephemeral-storage" id="variables-vm-vtsingledefault-resource-limit-ephemeral-storage">#</a> |
| VM_VTSINGLEDEFAULT_RESOURCE_REQUEST_MEM: `500Mi` <a href="#variables-vm-vtsingledefault-resource-request-mem" id="variables-vm-vtsingledefault-resource-request-mem">#</a> |
| VM_VTSINGLEDEFAULT_RESOURCE_REQUEST_CPU: `150m` <a href="#variables-vm-vtsingledefault-resource-request-cpu" id="variables-vm-vtsingledefault-resource-request-cpu">#</a> |
| VM_VTSINGLEDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtsingledefault-resource-request-ephemeral-storage" id="variables-vm-vtsingledefault-resource-request-ephemeral-storage">#</a> |
| VM_VMALERTDEFAULT_IMAGE: `victoriametrics/vmalert` <a href="#variables-vm-vmalertdefault-image" id="variables-vm-vmalertdefault-image">#</a> |
| VM_VMALERTDEFAULT_VERSION: `${VM_METRICS_VERSION}` <a href="#variables-vm-vmalertdefault-version" id="variables-vm-vmalertdefault-version">#</a> |
| VM_VMALERTDEFAULT_PORT: `8080` <a href="#variables-vm-vmalertdefault-port" id="variables-vm-vmalertdefault-port">#</a> |
| VM_VMALERTDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmalertdefault-usedefaultresources" id="variables-vm-vmalertdefault-usedefaultresources">#</a> |
| VM_VMALERTDEFAULT_RESOURCE_LIMIT_MEM: `500Mi` <a href="#variables-vm-vmalertdefault-resource-limit-mem" id="variables-vm-vmalertdefault-resource-limit-mem">#</a> |
| VM_VMALERTDEFAULT_RESOURCE_LIMIT_CPU: `200m` <a href="#variables-vm-vmalertdefault-resource-limit-cpu" id="variables-vm-vmalertdefault-resource-limit-cpu">#</a> |
| VM_VMALERTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmalertdefault-resource-limit-ephemeral-storage" id="variables-vm-vmalertdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMALERTDEFAULT_RESOURCE_REQUEST_MEM: `200Mi` <a href="#variables-vm-vmalertdefault-resource-request-mem" id="variables-vm-vmalertdefault-resource-request-mem">#</a> |
| VM_VMALERTDEFAULT_RESOURCE_REQUEST_CPU: `50m` <a href="#variables-vm-vmalertdefault-resource-request-cpu" id="variables-vm-vmalertdefault-resource-request-cpu">#</a> |
| VM_VMALERTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmalertdefault-resource-request-ephemeral-storage" id="variables-vm-vmalertdefault-resource-request-ephemeral-storage">#</a> |
| VM_VMSERVICESCRAPEDEFAULT_ENFORCEENDPOINTSLICES: `false` <a href="#variables-vm-vmservicescrapedefault-enforceendpointslices" id="variables-vm-vmservicescrapedefault-enforceendpointslices">#</a><br>Use endpointslices instead of endpoints as discovery role for vmservicescrape when generate scrape config for vmagent. |
| VM_VMAGENTDEFAULT_IMAGE: `victoriametrics/vmagent` <a href="#variables-vm-vmagentdefault-image" id="variables-vm-vmagentdefault-image">#</a> |
| VM_VMAGENTDEFAULT_VERSION: `${VM_METRICS_VERSION}` <a href="#variables-vm-vmagentdefault-version" id="variables-vm-vmagentdefault-version">#</a> |
| VM_VMAGENTDEFAULT_PORT: `8429` <a href="#variables-vm-vmagentdefault-port" id="variables-vm-vmagentdefault-port">#</a> |
| VM_VMAGENTDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmagentdefault-usedefaultresources" id="variables-vm-vmagentdefault-usedefaultresources">#</a> |
| VM_VMAGENTDEFAULT_RESOURCE_LIMIT_MEM: `500Mi` <a href="#variables-vm-vmagentdefault-resource-limit-mem" id="variables-vm-vmagentdefault-resource-limit-mem">#</a> |
| VM_VMAGENTDEFAULT_RESOURCE_LIMIT_CPU: `200m` <a href="#variables-vm-vmagentdefault-resource-limit-cpu" id="variables-vm-vmagentdefault-resource-limit-cpu">#</a> |
| VM_VMAGENTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmagentdefault-resource-limit-ephemeral-storage" id="variables-vm-vmagentdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMAGENTDEFAULT_RESOURCE_REQUEST_MEM: `200Mi` <a href="#variables-vm-vmagentdefault-resource-request-mem" id="variables-vm-vmagentdefault-resource-request-mem">#</a> |
| VM_VMAGENTDEFAULT_RESOURCE_REQUEST_CPU: `50m` <a href="#variables-vm-vmagentdefault-resource-request-cpu" id="variables-vm-vmagentdefault-resource-request-cpu">#</a> |
| VM_VMAGENTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmagentdefault-resource-request-ephemeral-storage" id="variables-vm-vmagentdefault-resource-request-ephemeral-storage">#</a> |
| VM_VMANOMALYDEFAULT_IMAGE: `victoriametrics/vmanomaly` <a href="#variables-vm-vmanomalydefault-image" id="variables-vm-vmanomalydefault-image">#</a> |
| VM_VMANOMALYDEFAULT_VERSION: `${VM_ANOMALY_VERSION}` <a href="#variables-vm-vmanomalydefault-version" id="variables-vm-vmanomalydefault-version">#</a> |
| VM_VMANOMALYDEFAULT_PORT: `8490` <a href="#variables-vm-vmanomalydefault-port" id="variables-vm-vmanomalydefault-port">#</a> |
| VM_VMANOMALYDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmanomalydefault-usedefaultresources" id="variables-vm-vmanomalydefault-usedefaultresources">#</a> |
| VM_VMANOMALYDEFAULT_RESOURCE_LIMIT_MEM: `500Mi` <a href="#variables-vm-vmanomalydefault-resource-limit-mem" id="variables-vm-vmanomalydefault-resource-limit-mem">#</a> |
| VM_VMANOMALYDEFAULT_RESOURCE_LIMIT_CPU: `200m` <a href="#variables-vm-vmanomalydefault-resource-limit-cpu" id="variables-vm-vmanomalydefault-resource-limit-cpu">#</a> |
| VM_VMANOMALYDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmanomalydefault-resource-limit-ephemeral-storage" id="variables-vm-vmanomalydefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMANOMALYDEFAULT_RESOURCE_REQUEST_MEM: `200Mi` <a href="#variables-vm-vmanomalydefault-resource-request-mem" id="variables-vm-vmanomalydefault-resource-request-mem">#</a> |
| VM_VMANOMALYDEFAULT_RESOURCE_REQUEST_CPU: `50m` <a href="#variables-vm-vmanomalydefault-resource-request-cpu" id="variables-vm-vmanomalydefault-resource-request-cpu">#</a> |
| VM_VMANOMALYDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmanomalydefault-resource-request-ephemeral-storage" id="variables-vm-vmanomalydefault-resource-request-ephemeral-storage">#</a> |
| VM_VMSINGLEDEFAULT_IMAGE: `victoriametrics/victoria-metrics` <a href="#variables-vm-vmsingledefault-image" id="variables-vm-vmsingledefault-image">#</a> |
| VM_VMSINGLEDEFAULT_VERSION: `${VM_METRICS_VERSION}` <a href="#variables-vm-vmsingledefault-version" id="variables-vm-vmsingledefault-version">#</a> |
| VM_VMSINGLEDEFAULT_PORT: `8429` <a href="#variables-vm-vmsingledefault-port" id="variables-vm-vmsingledefault-port">#</a> |
| VM_VMSINGLEDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmsingledefault-usedefaultresources" id="variables-vm-vmsingledefault-usedefaultresources">#</a> |
| VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM: `1500Mi` <a href="#variables-vm-vmsingledefault-resource-limit-mem" id="variables-vm-vmsingledefault-resource-limit-mem">#</a> |
| VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU: `1200m` <a href="#variables-vm-vmsingledefault-resource-limit-cpu" id="variables-vm-vmsingledefault-resource-limit-cpu">#</a> |
| VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmsingledefault-resource-limit-ephemeral-storage" id="variables-vm-vmsingledefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_MEM: `500Mi` <a href="#variables-vm-vmsingledefault-resource-request-mem" id="variables-vm-vmsingledefault-resource-request-mem">#</a> |
| VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_CPU: `150m` <a href="#variables-vm-vmsingledefault-resource-request-cpu" id="variables-vm-vmsingledefault-resource-request-cpu">#</a> |
| VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmsingledefault-resource-request-ephemeral-storage" id="variables-vm-vmsingledefault-resource-request-ephemeral-storage">#</a> |
| VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmclusterdefault-usedefaultresources" id="variables-vm-vmclusterdefault-usedefaultresources">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_IMAGE: `victoriametrics/vmselect` <a href="#variables-vm-vmclusterdefault-vmselectdefault-image" id="variables-vm-vmclusterdefault-vmselectdefault-image">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_VERSION: `${VM_METRICS_VERSION}-cluster` <a href="#variables-vm-vmclusterdefault-vmselectdefault-version" id="variables-vm-vmclusterdefault-vmselectdefault-version">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_PORT: `8481` <a href="#variables-vm-vmclusterdefault-vmselectdefault-port" id="variables-vm-vmclusterdefault-vmselectdefault-port">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_MEM: `1000Mi` <a href="#variables-vm-vmclusterdefault-vmselectdefault-resource-limit-mem" id="variables-vm-vmclusterdefault-vmselectdefault-resource-limit-mem">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_CPU: `500m` <a href="#variables-vm-vmclusterdefault-vmselectdefault-resource-limit-cpu" id="variables-vm-vmclusterdefault-vmselectdefault-resource-limit-cpu">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmclusterdefault-vmselectdefault-resource-limit-ephemeral-storage" id="variables-vm-vmclusterdefault-vmselectdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_MEM: `500Mi` <a href="#variables-vm-vmclusterdefault-vmselectdefault-resource-request-mem" id="variables-vm-vmclusterdefault-vmselectdefault-resource-request-mem">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_CPU: `100m` <a href="#variables-vm-vmclusterdefault-vmselectdefault-resource-request-cpu" id="variables-vm-vmclusterdefault-vmselectdefault-resource-request-cpu">#</a> |
| VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmclusterdefault-vmselectdefault-resource-request-ephemeral-storage" id="variables-vm-vmclusterdefault-vmselectdefault-resource-request-ephemeral-storage">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_IMAGE: `victoriametrics/vmstorage` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-image" id="variables-vm-vmclusterdefault-vmstoragedefault-image">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_VERSION: `${VM_METRICS_VERSION}-cluster` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-version" id="variables-vm-vmclusterdefault-vmstoragedefault-version">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_VMINSERTPORT: `8400` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-vminsertport" id="variables-vm-vmclusterdefault-vmstoragedefault-vminsertport">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_VMSELECTPORT: `8401` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-vmselectport" id="variables-vm-vmclusterdefault-vmstoragedefault-vmselectport">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_PORT: `8482` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-port" id="variables-vm-vmclusterdefault-vmstoragedefault-port">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_MEM: `1500Mi` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-resource-limit-mem" id="variables-vm-vmclusterdefault-vmstoragedefault-resource-limit-mem">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-resource-limit-cpu" id="variables-vm-vmclusterdefault-vmstoragedefault-resource-limit-cpu">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-resource-limit-ephemeral-storage" id="variables-vm-vmclusterdefault-vmstoragedefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_MEM: `500Mi` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-resource-request-mem" id="variables-vm-vmclusterdefault-vmstoragedefault-resource-request-mem">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_CPU: `250m` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-resource-request-cpu" id="variables-vm-vmclusterdefault-vmstoragedefault-resource-request-cpu">#</a> |
| VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmclusterdefault-vmstoragedefault-resource-request-ephemeral-storage" id="variables-vm-vmclusterdefault-vmstoragedefault-resource-request-ephemeral-storage">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_IMAGE: `victoriametrics/vminsert` <a href="#variables-vm-vmclusterdefault-vminsertdefault-image" id="variables-vm-vmclusterdefault-vminsertdefault-image">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_VERSION: `${VM_METRICS_VERSION}-cluster` <a href="#variables-vm-vmclusterdefault-vminsertdefault-version" id="variables-vm-vmclusterdefault-vminsertdefault-version">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_PORT: `8480` <a href="#variables-vm-vmclusterdefault-vminsertdefault-port" id="variables-vm-vmclusterdefault-vminsertdefault-port">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_MEM: `500Mi` <a href="#variables-vm-vmclusterdefault-vminsertdefault-resource-limit-mem" id="variables-vm-vmclusterdefault-vminsertdefault-resource-limit-mem">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_CPU: `500m` <a href="#variables-vm-vmclusterdefault-vminsertdefault-resource-limit-cpu" id="variables-vm-vmclusterdefault-vminsertdefault-resource-limit-cpu">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmclusterdefault-vminsertdefault-resource-limit-ephemeral-storage" id="variables-vm-vmclusterdefault-vminsertdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_MEM: `200Mi` <a href="#variables-vm-vmclusterdefault-vminsertdefault-resource-request-mem" id="variables-vm-vmclusterdefault-vminsertdefault-resource-request-mem">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_CPU: `150m` <a href="#variables-vm-vmclusterdefault-vminsertdefault-resource-request-cpu" id="variables-vm-vmclusterdefault-vminsertdefault-resource-request-cpu">#</a> |
| VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmclusterdefault-vminsertdefault-resource-request-ephemeral-storage" id="variables-vm-vmclusterdefault-vminsertdefault-resource-request-ephemeral-storage">#</a> |
| VM_VMALERTMANAGER_ALERTMANAGERDEFAULTBASEIMAGE: `prom/alertmanager` <a href="#variables-vm-vmalertmanager-alertmanagerdefaultbaseimage" id="variables-vm-vmalertmanager-alertmanagerdefaultbaseimage">#</a> |
| VM_VMALERTMANAGER_ALERTMANAGERVERSION: `v0.31.0` <a href="#variables-vm-vmalertmanager-alertmanagerversion" id="variables-vm-vmalertmanager-alertmanagerversion">#</a> |
| VM_VMALERTMANAGER_PORT: `9093` <a href="#variables-vm-vmalertmanager-port" id="variables-vm-vmalertmanager-port">#</a> |
| VM_VMALERTMANAGER_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmalertmanager-usedefaultresources" id="variables-vm-vmalertmanager-usedefaultresources">#</a> |
| VM_VMALERTMANAGER_RESOURCE_LIMIT_MEM: `256Mi` <a href="#variables-vm-vmalertmanager-resource-limit-mem" id="variables-vm-vmalertmanager-resource-limit-mem">#</a> |
| VM_VMALERTMANAGER_RESOURCE_LIMIT_CPU: `100m` <a href="#variables-vm-vmalertmanager-resource-limit-cpu" id="variables-vm-vmalertmanager-resource-limit-cpu">#</a> |
| VM_VMALERTMANAGER_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmalertmanager-resource-limit-ephemeral-storage" id="variables-vm-vmalertmanager-resource-limit-ephemeral-storage">#</a> |
| VM_VMALERTMANAGER_RESOURCE_REQUEST_MEM: `56Mi` <a href="#variables-vm-vmalertmanager-resource-request-mem" id="variables-vm-vmalertmanager-resource-request-mem">#</a> |
| VM_VMALERTMANAGER_RESOURCE_REQUEST_CPU: `30m` <a href="#variables-vm-vmalertmanager-resource-request-cpu" id="variables-vm-vmalertmanager-resource-request-cpu">#</a> |
| VM_VMALERTMANAGER_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmalertmanager-resource-request-ephemeral-storage" id="variables-vm-vmalertmanager-resource-request-ephemeral-storage">#</a> |
| VM_DISABLESELFSERVICESCRAPECREATION: `false` <a href="#variables-vm-disableselfservicescrapecreation" id="variables-vm-disableselfservicescrapecreation">#</a> |
| VM_VMBACKUP_IMAGE: `victoriametrics/vmbackupmanager` <a href="#variables-vm-vmbackup-image" id="variables-vm-vmbackup-image">#</a> |
| VM_VMBACKUP_VERSION: `${VM_METRICS_VERSION}-enterprise` <a href="#variables-vm-vmbackup-version" id="variables-vm-vmbackup-version">#</a> |
| VM_VMBACKUP_PORT: `8300` <a href="#variables-vm-vmbackup-port" id="variables-vm-vmbackup-port">#</a> |
| VM_VMBACKUP_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmbackup-usedefaultresources" id="variables-vm-vmbackup-usedefaultresources">#</a> |
| VM_VMBACKUP_RESOURCE_LIMIT_MEM: `500Mi` <a href="#variables-vm-vmbackup-resource-limit-mem" id="variables-vm-vmbackup-resource-limit-mem">#</a> |
| VM_VMBACKUP_RESOURCE_LIMIT_CPU: `500m` <a href="#variables-vm-vmbackup-resource-limit-cpu" id="variables-vm-vmbackup-resource-limit-cpu">#</a> |
| VM_VMBACKUP_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmbackup-resource-limit-ephemeral-storage" id="variables-vm-vmbackup-resource-limit-ephemeral-storage">#</a> |
| VM_VMBACKUP_RESOURCE_REQUEST_MEM: `200Mi` <a href="#variables-vm-vmbackup-resource-request-mem" id="variables-vm-vmbackup-resource-request-mem">#</a> |
| VM_VMBACKUP_RESOURCE_REQUEST_CPU: `150m` <a href="#variables-vm-vmbackup-resource-request-cpu" id="variables-vm-vmbackup-resource-request-cpu">#</a> |
| VM_VMBACKUP_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmbackup-resource-request-ephemeral-storage" id="variables-vm-vmbackup-resource-request-ephemeral-storage">#</a> |
| VM_VMAUTHDEFAULT_IMAGE: `victoriametrics/vmauth` <a href="#variables-vm-vmauthdefault-image" id="variables-vm-vmauthdefault-image">#</a> |
| VM_VMAUTHDEFAULT_VERSION: `${VM_METRICS_VERSION}` <a href="#variables-vm-vmauthdefault-version" id="variables-vm-vmauthdefault-version">#</a> |
| VM_VMAUTHDEFAULT_PORT: `8427` <a href="#variables-vm-vmauthdefault-port" id="variables-vm-vmauthdefault-port">#</a> |
| VM_VMAUTHDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vmauthdefault-usedefaultresources" id="variables-vm-vmauthdefault-usedefaultresources">#</a> |
| VM_VMAUTHDEFAULT_RESOURCE_LIMIT_MEM: `300Mi` <a href="#variables-vm-vmauthdefault-resource-limit-mem" id="variables-vm-vmauthdefault-resource-limit-mem">#</a> |
| VM_VMAUTHDEFAULT_RESOURCE_LIMIT_CPU: `200m` <a href="#variables-vm-vmauthdefault-resource-limit-cpu" id="variables-vm-vmauthdefault-resource-limit-cpu">#</a> |
| VM_VMAUTHDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmauthdefault-resource-limit-ephemeral-storage" id="variables-vm-vmauthdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VMAUTHDEFAULT_RESOURCE_REQUEST_MEM: `100Mi` <a href="#variables-vm-vmauthdefault-resource-request-mem" id="variables-vm-vmauthdefault-resource-request-mem">#</a> |
| VM_VMAUTHDEFAULT_RESOURCE_REQUEST_CPU: `50m` <a href="#variables-vm-vmauthdefault-resource-request-cpu" id="variables-vm-vmauthdefault-resource-request-cpu">#</a> |
| VM_VMAUTHDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vmauthdefault-resource-request-ephemeral-storage" id="variables-vm-vmauthdefault-resource-request-ephemeral-storage">#</a> |
| VM_VLCLUSTERDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vlclusterdefault-usedefaultresources" id="variables-vm-vlclusterdefault-usedefaultresources">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_IMAGE: `victoriametrics/victoria-logs` <a href="#variables-vm-vlclusterdefault-vlselectdefault-image" id="variables-vm-vlclusterdefault-vlselectdefault-image">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_VERSION: `${VM_LOGS_VERSION}` <a href="#variables-vm-vlclusterdefault-vlselectdefault-version" id="variables-vm-vlclusterdefault-vlselectdefault-version">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_PORT: `9471` <a href="#variables-vm-vlclusterdefault-vlselectdefault-port" id="variables-vm-vlclusterdefault-vlselectdefault-port">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_RESOURCE_LIMIT_MEM: `1024Mi` <a href="#variables-vm-vlclusterdefault-vlselectdefault-resource-limit-mem" id="variables-vm-vlclusterdefault-vlselectdefault-resource-limit-mem">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vlclusterdefault-vlselectdefault-resource-limit-cpu" id="variables-vm-vlclusterdefault-vlselectdefault-resource-limit-cpu">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlclusterdefault-vlselectdefault-resource-limit-ephemeral-storage" id="variables-vm-vlclusterdefault-vlselectdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_RESOURCE_REQUEST_MEM: `256Mi` <a href="#variables-vm-vlclusterdefault-vlselectdefault-resource-request-mem" id="variables-vm-vlclusterdefault-vlselectdefault-resource-request-mem">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_RESOURCE_REQUEST_CPU: `100m` <a href="#variables-vm-vlclusterdefault-vlselectdefault-resource-request-cpu" id="variables-vm-vlclusterdefault-vlselectdefault-resource-request-cpu">#</a> |
| VM_VLCLUSTERDEFAULT_VLSELECTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlclusterdefault-vlselectdefault-resource-request-ephemeral-storage" id="variables-vm-vlclusterdefault-vlselectdefault-resource-request-ephemeral-storage">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_IMAGE: `victoriametrics/victoria-logs` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-image" id="variables-vm-vlclusterdefault-vlstoragedefault-image">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_VERSION: `${VM_LOGS_VERSION}` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-version" id="variables-vm-vlclusterdefault-vlstoragedefault-version">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_PORT: `9491` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-port" id="variables-vm-vlclusterdefault-vlstoragedefault-port">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_LIMIT_MEM: `2048Mi` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-resource-limit-mem" id="variables-vm-vlclusterdefault-vlstoragedefault-resource-limit-mem">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-resource-limit-cpu" id="variables-vm-vlclusterdefault-vlstoragedefault-resource-limit-cpu">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-resource-limit-ephemeral-storage" id="variables-vm-vlclusterdefault-vlstoragedefault-resource-limit-ephemeral-storage">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_MEM: `512Mi` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-resource-request-mem" id="variables-vm-vlclusterdefault-vlstoragedefault-resource-request-mem">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_CPU: `200m` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-resource-request-cpu" id="variables-vm-vlclusterdefault-vlstoragedefault-resource-request-cpu">#</a> |
| VM_VLCLUSTERDEFAULT_VLSTORAGEDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlclusterdefault-vlstoragedefault-resource-request-ephemeral-storage" id="variables-vm-vlclusterdefault-vlstoragedefault-resource-request-ephemeral-storage">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_IMAGE: `victoriametrics/victoria-logs` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-image" id="variables-vm-vlclusterdefault-vlinsertdefault-image">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_VERSION: `${VM_LOGS_VERSION}` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-version" id="variables-vm-vlclusterdefault-vlinsertdefault-version">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_PORT: `9481` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-port" id="variables-vm-vlclusterdefault-vlinsertdefault-port">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_RESOURCE_LIMIT_MEM: `1024Mi` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-resource-limit-mem" id="variables-vm-vlclusterdefault-vlinsertdefault-resource-limit-mem">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-resource-limit-cpu" id="variables-vm-vlclusterdefault-vlinsertdefault-resource-limit-cpu">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-resource-limit-ephemeral-storage" id="variables-vm-vlclusterdefault-vlinsertdefault-resource-limit-ephemeral-storage">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_RESOURCE_REQUEST_MEM: `256Mi` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-resource-request-mem" id="variables-vm-vlclusterdefault-vlinsertdefault-resource-request-mem">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_RESOURCE_REQUEST_CPU: `100m` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-resource-request-cpu" id="variables-vm-vlclusterdefault-vlinsertdefault-resource-request-cpu">#</a> |
| VM_VLCLUSTERDEFAULT_VLINSERTDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vlclusterdefault-vlinsertdefault-resource-request-ephemeral-storage" id="variables-vm-vlclusterdefault-vlinsertdefault-resource-request-ephemeral-storage">#</a> |
| VM_VTCLUSTERDEFAULT_USEDEFAULTRESOURCES: `true` <a href="#variables-vm-vtclusterdefault-usedefaultresources" id="variables-vm-vtclusterdefault-usedefaultresources">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_IMAGE: `victoriametrics/victoria-traces` <a href="#variables-vm-vtclusterdefault-select-image" id="variables-vm-vtclusterdefault-select-image">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_VERSION: `${VM_TRACES_VERSION}` <a href="#variables-vm-vtclusterdefault-select-version" id="variables-vm-vtclusterdefault-select-version">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_PORT: `10471` <a href="#variables-vm-vtclusterdefault-select-port" id="variables-vm-vtclusterdefault-select-port">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_RESOURCE_LIMIT_MEM: `1024Mi` <a href="#variables-vm-vtclusterdefault-select-resource-limit-mem" id="variables-vm-vtclusterdefault-select-resource-limit-mem">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vtclusterdefault-select-resource-limit-cpu" id="variables-vm-vtclusterdefault-select-resource-limit-cpu">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtclusterdefault-select-resource-limit-ephemeral-storage" id="variables-vm-vtclusterdefault-select-resource-limit-ephemeral-storage">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_RESOURCE_REQUEST_MEM: `256Mi` <a href="#variables-vm-vtclusterdefault-select-resource-request-mem" id="variables-vm-vtclusterdefault-select-resource-request-mem">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_RESOURCE_REQUEST_CPU: `100m` <a href="#variables-vm-vtclusterdefault-select-resource-request-cpu" id="variables-vm-vtclusterdefault-select-resource-request-cpu">#</a> |
| VM_VTCLUSTERDEFAULT_SELECT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtclusterdefault-select-resource-request-ephemeral-storage" id="variables-vm-vtclusterdefault-select-resource-request-ephemeral-storage">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_IMAGE: `victoriametrics/victoria-traces` <a href="#variables-vm-vtclusterdefault-storage-image" id="variables-vm-vtclusterdefault-storage-image">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_VERSION: `${VM_TRACES_VERSION}` <a href="#variables-vm-vtclusterdefault-storage-version" id="variables-vm-vtclusterdefault-storage-version">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_PORT: `10491` <a href="#variables-vm-vtclusterdefault-storage-port" id="variables-vm-vtclusterdefault-storage-port">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_LIMIT_MEM: `2048Mi` <a href="#variables-vm-vtclusterdefault-storage-resource-limit-mem" id="variables-vm-vtclusterdefault-storage-resource-limit-mem">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vtclusterdefault-storage-resource-limit-cpu" id="variables-vm-vtclusterdefault-storage-resource-limit-cpu">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtclusterdefault-storage-resource-limit-ephemeral-storage" id="variables-vm-vtclusterdefault-storage-resource-limit-ephemeral-storage">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_REQUEST_MEM: `512Mi` <a href="#variables-vm-vtclusterdefault-storage-resource-request-mem" id="variables-vm-vtclusterdefault-storage-resource-request-mem">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_REQUEST_CPU: `200m` <a href="#variables-vm-vtclusterdefault-storage-resource-request-cpu" id="variables-vm-vtclusterdefault-storage-resource-request-cpu">#</a> |
| VM_VTCLUSTERDEFAULT_STORAGE_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtclusterdefault-storage-resource-request-ephemeral-storage" id="variables-vm-vtclusterdefault-storage-resource-request-ephemeral-storage">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_IMAGE: `victoriametrics/victoria-traces` <a href="#variables-vm-vtclusterdefault-insert-image" id="variables-vm-vtclusterdefault-insert-image">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_VERSION: `${VM_TRACES_VERSION}` <a href="#variables-vm-vtclusterdefault-insert-version" id="variables-vm-vtclusterdefault-insert-version">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_PORT: `10481` <a href="#variables-vm-vtclusterdefault-insert-port" id="variables-vm-vtclusterdefault-insert-port">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_RESOURCE_LIMIT_MEM: `1024Mi` <a href="#variables-vm-vtclusterdefault-insert-resource-limit-mem" id="variables-vm-vtclusterdefault-insert-resource-limit-mem">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_RESOURCE_LIMIT_CPU: `1000m` <a href="#variables-vm-vtclusterdefault-insert-resource-limit-cpu" id="variables-vm-vtclusterdefault-insert-resource-limit-cpu">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_RESOURCE_LIMIT_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtclusterdefault-insert-resource-limit-ephemeral-storage" id="variables-vm-vtclusterdefault-insert-resource-limit-ephemeral-storage">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_RESOURCE_REQUEST_MEM: `256Mi` <a href="#variables-vm-vtclusterdefault-insert-resource-request-mem" id="variables-vm-vtclusterdefault-insert-resource-request-mem">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_RESOURCE_REQUEST_CPU: `100m` <a href="#variables-vm-vtclusterdefault-insert-resource-request-cpu" id="variables-vm-vtclusterdefault-insert-resource-request-cpu">#</a> |
| VM_VTCLUSTERDEFAULT_INSERT_RESOURCE_REQUEST_EPHEMERAL_STORAGE: `unlimited` <a href="#variables-vm-vtclusterdefault-insert-resource-request-ephemeral-storage" id="variables-vm-vtclusterdefault-insert-resource-request-ephemeral-storage">#</a> |
| VM_ENABLEDPROMETHEUSCONVERTER_PODMONITOR: `true` <a href="#variables-vm-enabledprometheusconverter-podmonitor" id="variables-vm-enabledprometheusconverter-podmonitor">#</a> |
| VM_ENABLEDPROMETHEUSCONVERTER_SERVICESCRAPE: `true` <a href="#variables-vm-enabledprometheusconverter-servicescrape" id="variables-vm-enabledprometheusconverter-servicescrape">#</a> |
| VM_ENABLEDPROMETHEUSCONVERTER_PROMETHEUSRULE: `true` <a href="#variables-vm-enabledprometheusconverter-prometheusrule" id="variables-vm-enabledprometheusconverter-prometheusrule">#</a> |
| VM_ENABLEDPROMETHEUSCONVERTER_PROBE: `true` <a href="#variables-vm-enabledprometheusconverter-probe" id="variables-vm-enabledprometheusconverter-probe">#</a> |
| VM_ENABLEDPROMETHEUSCONVERTER_ALERTMANAGERCONFIG: `true` <a href="#variables-vm-enabledprometheusconverter-alertmanagerconfig" id="variables-vm-enabledprometheusconverter-alertmanagerconfig">#</a> |
| VM_ENABLEDPROMETHEUSCONVERTER_SCRAPECONFIG: `true` <a href="#variables-vm-enabledprometheusconverter-scrapeconfig" id="variables-vm-enabledprometheusconverter-scrapeconfig">#</a> |
| VM_PROMETHEUSCONVERTERADDARGOCDIGNOREANNOTATIONS: `false` <a href="#variables-vm-prometheusconverteraddargocdignoreannotations" id="variables-vm-prometheusconverteraddargocdignoreannotations">#</a><br>adds compare-options and sync-options for prometheus objects converted by operator. It helps to properly use converter with ArgoCD |
| VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES: `false` <a href="#variables-vm-enabledprometheusconverterownerreferences" id="variables-vm-enabledprometheusconverterownerreferences">#</a> |
| VM_FILTERPROMETHEUSCONVERTERLABELPREFIXES: `-` <a href="#variables-vm-filterprometheusconverterlabelprefixes" id="variables-vm-filterprometheusconverterlabelprefixes">#</a><br>allows filtering for converted labels, labels with matched prefix will be ignored |
| VM_FILTERPROMETHEUSCONVERTERANNOTATIONPREFIXES: `-` <a href="#variables-vm-filterprometheusconverterannotationprefixes" id="variables-vm-filterprometheusconverterannotationprefixes">#</a><br>allows filtering for converted annotations, annotations with matched prefix will be ignored |
| VM_CLUSTERDOMAINNAME: `-` <a href="#variables-vm-clusterdomainname" id="variables-vm-clusterdomainname">#</a><br>Defines domain name suffix for in-cluster addresses most known ClusterDomainName is .cluster.local |
| VM_APPREADYTIMEOUT: `80s` <a href="#variables-vm-appreadytimeout" id="variables-vm-appreadytimeout">#</a><br>Defines deadline for deployment/statefulset to transit into ready state to wait for transition to ready state |
| VM_PODWAITREADYTIMEOUT: `80s` <a href="#variables-vm-podwaitreadytimeout" id="variables-vm-podwaitreadytimeout">#</a><br>Defines single pod deadline to wait for transition to ready state |
| VM_PODWAITREADYINTERVALCHECK: `5s` <a href="#variables-vm-podwaitreadyintervalcheck" id="variables-vm-podwaitreadyintervalcheck">#</a><br>Defines poll interval for pods ready check at statefulset rollout update |
| VM_FORCERESYNCINTERVAL: `60s` <a href="#variables-vm-forceresyncinterval" id="variables-vm-forceresyncinterval">#</a><br>configures force resync interval for VMAgent, VMAlert, VMAlertmanager and VMAuth. |
| VM_ENABLESTRICTSECURITY: `false` <a href="#variables-vm-enablestrictsecurity" id="variables-vm-enablestrictsecurity">#</a><br>EnableStrictSecurity will add default `securityContext` to pods and containers created by operator Default PodSecurityContext include: 1. RunAsNonRoot: true 2. RunAsUser/RunAsGroup/FSGroup: 65534 '65534' refers to 'nobody' in all the used default images like alpine, busybox. If you're using customize image, please make sure '65534' is a valid uid in there or specify SecurityContext. 3. FSGroupChangePolicy: &onRootMismatch If KubeVersion>=1.20, use `FSGroupChangePolicy="onRootMismatch"` to skip the recursive permission change when the root of the volume already has the correct permissions 4. SeccompProfile:      type: RuntimeDefault Use `RuntimeDefault` seccomp profile by default, which is defined by the container runtime, instead of using the Unconfined (seccomp disabled) mode. Default container SecurityContext include: 1. AllowPrivilegeEscalation: false 2. ReadOnlyRootFilesystem: true 3. Capabilities:      drop:        - all turn off `EnableStrictSecurity` by default, see https://github.com/VictoriaMetrics/operator/issues/749 for details |
