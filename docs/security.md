---
weight: 3
title: Security
menu:
  docs:
    parent: "operator"
    weight: 3
aliases:
  - /operator/security/
  - /operator/security/index.html
tags:
  - kubernetes
  - logs
  - metrics
---
## Access control

### Roles

To run in a cluster the operator needs certain permissions, you can see them in [this directory](https://github.com/VictoriaMetrics/operator/tree/master/config/rbac):

- [`role.yaml` file](https://github.com/VictoriaMetrics/operator/blob/master/config/rbac/role.yaml) - basic set of cluster roles for launching an operator.
- [`leader_election_role.yaml` file](https://github.com/VictoriaMetrics/operator/blob/master/config/rbac/leader_election_role.yaml) - set of roles with permissions to do leader election (is necessary to run the operator in several replicas for high availability).

Also, you can use single-namespace mode with minimal permissions, see [this section](https://docs.victoriametrics.com/operator/configuration/#namespaced-mode) for details.

### Managed component RBAC

`VMAgent` uses Kubernetes service discovery for scrape targets, so its `ServiceAccount` needs read-only access to Kubernetes discovery objects.
The example [`vmagent_rbac.yaml` file](https://github.com/VictoriaMetrics/operator/blob/master/config/examples/vmagent_rbac.yaml) creates a `ClusterRole` with these permissions:

- `get`, `list`, and `watch` for `nodes`, `nodes/metrics`, `services`, `endpoints`, `endpointslices`, `pods`, and `ingresses` in core, `networking.k8s.io`, and `discovery.k8s.io` API groups. These permissions allow vmagent to discover scrape targets across the cluster.
- `get` for `namespaces` and `configmaps`. These permissions allow namespace metadata lookups and config map reads needed by discovery configuration.
- `get` for non-resource URLs `/metrics`, `/metrics/resources`, and `/metrics/slis`. These permissions allow scraping Kubernetes API server metrics endpoints when configured.
- `get` for OpenShift's `routers/metrics` and `registry/metrics` resources in `route.openshift.io` and `image.openshift.io` API groups. These permissions allow scraping OpenShift router and registry metrics when configured.

`VMSingle` can use the same Kubernetes service discovery as `VMAgent` when it scrapes targets directly.
When `spec.ingestOnlyMode` is disabled, operator creates the same cluster-wide discovery `ClusterRole` and `ClusterRoleBinding` for `VMSingle`.
When `spec.ingestOnlyMode` is enabled, `VMSingle` doesn't need service discovery permissions and operator doesn't add these cluster-wide rules.

`VLAgent` can collect Kubernetes logs by watching pods, nodes, and namespaces.
When cluster-wide access is allowed, operator creates a `ClusterRole` with `get`, `list`, and `watch` permissions for `nodes`, `pods`, and `namespaces` in the core API group.
When the operator runs in namespaced mode, operator skips the `VLAgent` `ClusterRole` and `ClusterRoleBinding`; configure namespaced RBAC manually if `VLAgent` still needs Kubernetes API access.

`VMAuth` and `VMAlertmanager` don't need cluster-wide RBAC for their own runtime configuration reload.
Operator creates namespaced `Role` and `RoleBinding` objects with `get`, `list`, and `watch` permissions for `secrets` in their own namespace, so the config-reloader can watch generated configuration secrets.

For example, this `VMSingle` needs scrape discovery RBAC because it scrapes targets directly:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: scrape
  namespace: monitoring
spec:
  ingestOnlyMode: false
  selectAllByDefault: true
```

This `VMSingle` doesn't need scrape discovery RBAC, because it only receives ingested data:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: ingest
  namespace: monitoring
spec:
  ingestOnlyMode: true
```

This `VLAgent` needs Kubernetes API access for pod metadata and log target discovery:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VLAgent
metadata:
  name: logs
  namespace: monitoring
spec:
  remoteWrite:
    - url: http://vlinsert-vlogs.monitoring.svc:9428/insert/jsonline
```

With cluster-wide operator access, it gets a `ClusterRole` similar to:

```yaml
rules:
  - apiGroups: [""]
    resources: ["nodes", "pods", "namespaces"]
    verbs: ["get", "list", "watch"]
```

`VMAuth` and `VMAlertmanager` get namespace-scoped secret access similar to:

```yaml
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch"]
```

### Reducing API access scope
Cluster-wide discovery roles are created because Kubernetes service discovery can discover targets in multiple namespaces and cluster-scoped resources such as `nodes`.
To reduce API access scope, run the operator in namespaced mode with the `WATCH_NAMESPACE` environment variable.
When `WATCH_NAMESPACE` is set, operator avoids cluster-wide watches and limits object selection to the configured namespace list.
For `VMAgent` and `VMSingle`, operator creates namespaced `Role` and `RoleBinding` objects in watched namespaces so service discovery can work there without granting a cluster-wide `ClusterRole` for namespaced resources.
Their config and credential `secrets` access stays namespace-scoped even in cluster-wide mode.
Cluster-scoped discovery, such as `nodes` and `nodes/metrics`, still requires cluster-level permissions if enabled in scrape configuration.

For example, this operator watches only two namespaces:

```yaml
env:
  - name: WATCH_NAMESPACE
    value: team-a,team-b
```

In this mode, operator creates namespaced discovery `Role` and `RoleBinding` objects for `VMAgent` and `VMSingle` in `team-a` and `team-b` instead of cluster-wide discovery RBAC for namespaced resources.

### Namespace isolation with enforced labels

`enforcedNamespaceLabel` allows enforcing the namespace of the user-created operator object on metrics, alerting rules, and alert routing configuration.
It is useful for building isolated environments where users manage monitoring objects in their own namespaces, while platform administrators run shared monitoring infrastructure. When the team creates `VMServiceScrape`, `VMRule`, or `VMAlertmanagerConfig` in namespace `team-a`, operator automatically adds or matches `tenant="team-a"` label, so metrics, rules, and alerts stay logically (not physically) isolated from other teams.

The field can be configured on these objects:
- [`VMAgent`](https://docs.victoriametrics.com/operator/resources/vmagent/) and [`VMSingle`](https://docs.victoriametrics.com/operator/resources/vmsingle/) enforce the namespace label for metrics collected from `VMServiceScrape`, `VMPodScrape`, `VMNodeScrape`, `VMStaticScrape`, `VMProbe`, and `VMScrapeConfig` objects.
- [`VMAlert`](https://docs.victoriametrics.com/operator/resources/vmalert/) enforces the namespace label for rules loaded from `VMRule` objects.
- [`VMAlertmanager`](https://docs.victoriametrics.com/operator/resources/vmalertmanager/) uses the configured label name as the top-level route matcher for `VMAlertmanagerConfig` objects. If the field is not configured, the route matcher uses the `namespace` label.

For scrape objects, the operator appends the enforced namespace relabeling as the final `relabel_configs` rule.
This prevents users from overriding the enforced label with custom target relabeling.
The operator also ignores `metric_relabel_configs` rules that try to write into the enforced label.

For example, this `VMAgent` selects user scrape objects from all namespaces but always writes the source CRD namespace into the `tenant` label:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: shared
  namespace: monitoring-system
spec:
  enforcedNamespaceLabel: tenant
  selectAllByDefault: true
  remoteWrite:
    - url: http://vminsert-vmcluster.monitoring-system.svc:8480/insert/0/prometheus/api/v1/write
```

If user `team-a` creates a `VMServiceScrape` in namespace `team-a`, all metrics collected through this object get `tenant="team-a"`.
If user `team-b` creates scrape objects in namespace `team-b`, their metrics get `tenant="team-b"`.
Users cannot change this label through scrape relabeling or metric relabeling in their scrape objects.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: app
  namespace: team-a
spec:
  selector:
    matchLabels:
      app: app
  endpoints:
    - port: http
```

`VMAlert` can use the same enforced label for rules:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: shared
  namespace: monitoring-system
spec:
  enforcedNamespaceLabel: tenant
  selectAllByDefault: true
  datasource:
    url: http://vmselect-vmcluster.monitoring-system.svc:8481/select/0/prometheus
  notifier:
    url: http://vmalertmanager-shared.monitoring-system.svc:9093
```

If user `team-a` creates a `VMRule` in namespace `team-a`, operator adds `tenant="team-a"` to every generated alerting and recording rule from this object.

`VMAlertmanager` can use the same label to isolate alert routing configuration:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: shared
  namespace: monitoring-system
spec:
  enforcedNamespaceLabel: tenant
  selectAllByDefault: true
```

With this configuration, a `VMAlertmanagerConfig` from namespace `team-a` is converted into Alertmanager routes that match only alerts with `tenant="team-a"`.
Routes from namespace `team-b` match only `tenant="team-b"` alerts.
This allows teams to manage their own receivers and routes without receiving alerts from other namespaces.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanagerConfig
metadata:
  name: team-routing
  namespace: team-a
spec:
  route:
    receiver: team-a
  receivers:
    - name: team-a
      slackConfigs:
        - apiURL:
            key: url
            name: team-a-slack
          channel: '#team-a-alerts'
```

Use this pattern to build a managed monitoring service:

- Platform administrators deploy shared `VMAgent`, `VMAlert`, `VMAlertmanager`, and VictoriaMetrics storage in an infrastructure namespace.
- Administrators set the same `enforcedNamespaceLabel` value, such as `tenant`, on shared collection, rule evaluation, and alert routing objects.
- Users receive Kubernetes RBAC permissions to create only namespaced CRDs, such as `VMServiceScrape`, `VMPodScrape`, `VMRule`, and `VMAlertmanagerConfig`, in their own namespaces.
- Shared components select user objects with `selectAllByDefault` or explicit namespace and label selectors.
- Dashboards, alerts, and optional query gateways filter by the enforced label, for example `tenant="team-a"`.

This pattern gives every namespace isolated monitoring configuration while reusing one operator-managed backend.

Note that the separation is logical, not physical - so it additionally needs to be enforced by RBAC and query authentication via vmauth or another query gateway.

`enforcedNamespaceLabel` can be combined with `ignoreNamespaceSelectors: true` on `VMAgent` or `VMSingle` to restrict scrape objects from discovering targets in other namespaces. In this mode, scrape objects can discover endpoints only within their own namespace.
## Security policies

VictoriaMetrics operator provides several security features. First of all, it's built-in hardening configuration.

Environment variable `VM_ENABLESTRICTSECURITY=true` applies generic security options to the all created resources.

Such as `PodSecurityContext` and `SecurityContext` per `Container` 

```yaml
  securityContext:
    // '65534' refers to 'nobody' in all the used default images like alpine, busybox.
    fsGroup: 65534
    fsGroupChangePolicy: OnRootMismatch
    runAsGroup: 65534
    runAsNonRoot: true
    runAsUser: 65534
    seccompProfile:
      type: RuntimeDefault
```


 It's also possible to config strict security on resource basis:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: strict-security
  namespace: monitoring-system
spec:
  retentionPeriod: "2"
  removePvcAfterDelete: true
  useStrictSecurity: true
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 25Gi
```

 In addition, operator supports more granular per resource security configuration with [spec.securityContext](https://docs.victoriametrics.com/operator/api/#v1beta1-securitycontext) and [ContainerSecurityContext](https://docs.victoriametrics.com/operator/api/#v1beta1-containersecuritycontext)

### Pod SecurityContext

1. **RunAsNonRoot: true**
1. **RunAsUser/RunAsGroup/FSGroup: 65534**

    '65534' refers to 'nobody' in all the used default images like alpine, busybox.

    If you're using customize image, please make sure '65534' is a valid uid in there or specify SecurityContext.
1. **FSGroupChangePolicy: &onRootMismatch**
  
    If KubeVersion>=1.20, use `FSGroupChangePolicy="onRootMismatch"` to skip the recursive permission change
    when the root of the volume already has the correct permissions
1. **SeccompProfile: {type: RuntimeDefault}**

    Use `RuntimeDefault` seccomp profile by default, which is defined by the container runtime,
    instead of using the Unconfined (seccomp disabled) mode.

### Container SecurityContext

1. **AllowPrivilegeEscalation: false**
1. **ReadOnlyRootFilesystem: true**
1. **Capabilities: {drop: [all]}**


Also `SecurityContext` can be configured with spec setting. It may be useful for mounted volumes, with `VMSingle` for example:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-f
  namespace: monitoring-system
spec:
  retentionPeriod: "2"
  removePvcAfterDelete: true
  securityContext:
      runAsUser: 1000
      fsGroup: 1000
      runAsGroup: 1000
  extraArgs:
    dedup.minScrapeInterval: 10s
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 25Gi
  resources:
    requests:
      cpu: "0.5"
      memory: "512Mi"
    limits:
      cpu: "1"
      memory: "1512Mi"
```

### Kubernetes API Access


 By default, operator configures Kubernetes API Access for all managed components with own `ServiceAccount`.
This behaviour can be altered with object configuration - `spec.disableAutomountServiceAccountToken: true` {{% available_from "v0.54.0" "operator" %}}. See the
following [Kubernetes doc](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#opt-out-of-api-credential-automounting) for details.

 Consider the following example for VMAgent:
```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example
  namespace: default
spec:
  remoteWrite:
  - url: http://vmsingle-vms-victoria-metrics-k8s-stack.default.svc:8428/api/v1/write
  replicaCount: 1
  selectAllByDefault: true
  statefulMode: true
```

 Kubernetes controller-manager creates the following `Pod` definition and attaches `volumes` and `volumeMounts` with serviceAccount token:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vmagent-example-0
  namespace: default
spec:
  containers:
  - args:
...
    name: config-reloader
    volumeMounts:
...
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-api-access-q44gh
      readOnly: true
  - args:
...
    name: vmagent
    volumeMounts:
...
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-api-access-q44gh
      readOnly: true
  dnsPolicy: ClusterFirst
  enableServiceLinks: true
  hostname: vmagent-example-0
  initContainers:
  - args:
...
    name: config-init
    volumeMounts:
...
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-api-access-q44gh
      readOnly: true

  serviceAccount: vmagent-example
  serviceAccountName: vmagent-example
  volumes:
...
  - emptyDir: {}
    name: persistent-queue-data
  - name: kube-api-access-q44gh
    projected:
      defaultMode: 420
      sources:
      - serviceAccountToken:
          expirationSeconds: 3607
          path: token
      - configMap:
          items:
          - key: ca.crt
            path: ca.crt
          name: kube-root-ca.crt
      - downwardAPI:
          items:
          - fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
            path: namespace
```

 If `disableAutomountServiceAccountToken: true` is set. Operator adds `volumes` and `volumeMounts` only if application explicitly requires access to Kubernetes API:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example
  namespace: default
spec:
  disableAutomountServiceAccountToken: true
  remoteWrite:
  - url: http://vmsingle-vms-victoria-metrics-k8s-stack.default.svc:8428/api/v1/write
  replicaCount: 1
  selectAllByDefault: true
  statefulMode: true
```

 And `Pod` definition no longer has `volumeMounts` with serviceAccountToken for `config-reloader` container:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vmagent-example-0
  namespace: default
spec:
  automountServiceAccountToken: false
  containers:
  - args:
    name: config-reloader
    volumeMounts:
    - mountPath: /etc/vmagent/config_out
      name: config-out
    - mountPath: /etc/vmagent/config
      name: config
  - args:
    name: vmagent
    terminationMessagePath: /dev/termination-log
    terminationMessagePolicy: FallbackToLogsOnError
    volumeMounts:
    - mountPath: /vmagent_pq/vmagent-remotewrite-data
      name: persistent-queue-data
...
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-api-access
      readOnly: true
  dnsPolicy: ClusterFirst
  enableServiceLinks: true
  hostname: vmagent-example-0
  initContainers:
  - args:
    name: config-init
    volumeMounts:
    - mountPath: /etc/vmagent/config
      name: config
    - mountPath: /etc/vmagent/config_out
      name: config-out
  serviceAccount: vmagent-example
  serviceAccountName: vmagent-example
  volumes:
...
  - name: kube-api-access
    projected:
      defaultMode: 420
      sources:
      - serviceAccountToken:
          expirationSeconds: 3600
          path: token
      - configMap:
          name: kube-root-ca.crt
      - downwardAPI:
          items:
          - fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
            path: namespace
  - emptyDir: {}
    name: persistent-queue-data
```

 The following containers needs access to Kubernetes API server:
* vmagent uses Kubernetes service-discovery for scrapping target metrics.
* config-reloader watches configuration secret and triggers application state config reload on change.

 It's also possible to mount `serviceAccountToken` manually to any component.
Consider the following example:
```yaml
# add Role and Rolebinding for `vmsingle-with-sidecar` ServiceAccount
# or provide specific serviceAccount via: `spec.serviceAccountName`
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: with-sidecar
  namespace: default
spec:
  retentionPeriod: 1
  disableAutomountServiceAccountToken: true
  containers:
  - name: side-car-with-api-access
    image: busybox
    command: ["/bin/sh"] 
    args: ["-c", "tail -f /dev/stdout"] 
    volumeMounts:
    - name: kube-api-access
      mountPath: /var/run/secrets/kubernetes.io/serviceaccount
  volumes:
  - name: kube-api-access
    projected:
      defaultMode: 420
      sources:
      - serviceAccountToken:
          expirationSeconds: 3600
          path: token
      - configMap:
          name: kube-root-ca.crt
      - downwardAPI:
          items:
          - fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
            path: namespace
```
