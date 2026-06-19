---
weight: 7
title: Authorization and exposing components
menu:
  docs:
    parent: "operator"
    weight: 7
aliases:
  - /operator/auth/
  - /operator/auth/index.html
tags:
  - kubernetes
  - logs
  - metrics
---
## Exposing components

CRD objects doesn't have `ingress` configuration. 
Instead, you can use [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth/) as proxy between ingress-controller and VictoriaMetrics components.

It adds missing authorization and access control features and enforces it.

Access can be given with [VMUser](https://docs.victoriametrics.com/operator/resources/vmuser/) definition. 

It supports basic auth and bearer token authentication:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: main-router
spec:
  userNamespaceSelector: {}
  userSelector: {}
  ingress: {}
```

Advanced configuration with cert-manager annotations:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: router-main
spec:
  podMetadata:
    labels:
      component: vmauth
  userSelector: {}
  userNamespaceSelector: {}
  replicaCount: 2
  resources:
    requests:
      cpu: "250m"
      memory: "350Mi"
    limits:
      cpu: "500m"
      memory: "850Mi"
  ingress:
    tlsSecretName: vmauth-tls
    annotations:
      cert-manager.io/cluster-issuer: base
    class_name: nginx
    tlsHosts:
      - vm-access.example.com
```

Simple static routing with read-only access to vmagent for username - `user-1` with password `Asafs124142`:

```yaml
# curl vmauth:8427/metrics -u 'user-1:Asafs124142'
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: user-1
spec:
  password: Asafs124142
  targetRefs:
    - static:
        url: http://vmagent-base.default.svc:8429
      paths: ["/targets/api/v1","/targets","/metrics"]
```

With bearer token access:

```yaml
# curl vmauth:8427/metrics -H 'Authorization: Bearer Asafs124142'
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: user-2
spec:
  bearerToken: Asafs124142
  targetRefs:
    - static:
        url: http://vmagent-base.default.svc:8429
      paths: ["/targets/api/v1","/targets","/metrics"]
```

It's also possible to use service discovery for objects:

```yaml
# curl vmauth:8427/metrics -H 'Authorization: Bearer Asafs124142'
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: user-3
spec:
  bearerToken: Asafs124142
  targetRefs:
    - crd:
        kind: VMAgent
        name: base
        namespace: default
      paths: ["/targets/api/v1","/targets","/metrics"]
```

### VMCluster

`VMCluster` exposes two user-facing services — `vminsert` (write path, port 8480) and `vmselect` (read path, port 8481).
Both can be routed through a single `VMAuth` ingress using `VMUser` objects.
See [VMCluster — Services and URLs](https://docs.victoriametrics.com/operator/resources/vmcluster/#services-and-urls) for the full list of service names and ports.

The example below exposes VMUI and the query API (tenant `0`) for public read-only access via ingress,
and write access for an authenticated user:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: cluster-proxy
  namespace: default
spec:
  selectAllByDefault: true
  ingress:
    class_name: nginx                      # change to your ingress class
    host: victoriametrics.example.org
  unauthorizedUserAccessSpec:
    targetRefs:
      - crd:
          kind: VMCluster/vmselect
          name: example
          namespace: default
        target_path_suffix: "/select/0"
        paths:
          - "/prometheus/.*"
          - "/vmui.*"
---
# Write access: Prometheus remote write
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: cluster-writer
  namespace: default
spec:
  username: writer
  generatePassword: true
  targetRefs:
    - crd:
        kind: VMCluster/vminsert
        name: example
        namespace: default
      target_path_suffix: "/insert/0"
      paths:
        - "/prometheus/.*"
```

Cluster components also support auto path generation for multi-tenant setups:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
 name: vmuser-tenant-1
spec:
 bearerToken: some-token
 targetRefs:
  - crd:
     kind: VMCluster/vminsert
     name: test-persistent
     namespace: default
    paths:
      - /
    target_path_suffix: "/insert/1"
    query_args:
      - name: extra_filters
        values:
          - '{__name__!="service_info"}'
  - crd:
     kind: VMCluster/vmselect
     name: test-persistent
     namespace: default
    paths:
      - /
    target_path_suffix: "/select/1"
  - static:
     url: http://vmselect-test-persistent.default.svc:8481/
    paths:
     - /internal/resetRollupResultCache
```

For each `VMUser` operator generates corresponding secret with username/password or bearer token at the same namespace as `VMUser`.

### VLCluster

`VLCluster` exposes two user-facing services — `vlinsert` (write path, port 9481) and `vlselect` (read path, port 9471).
Both can be routed through a single `VMAuth` ingress using `VMUser` objects.
See [VLCluster — Services and URLs](https://docs.victoriametrics.com/operator/resources/vlcluster/#services-and-urls) for the full list of service names and ports.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: logs-proxy
  namespace: default
spec:
  selectAllByDefault: true
  ingress:
    class_name: nginx                      # change to your ingress class
    host: victorialogs.example.org
  unauthorizedUserAccessSpec:
    targetRefs:
      - crd:
          kind: VLCluster/vlselect
          name: example
          namespace: default
        paths:
          - "/select/.*"
---
# Write access for log shippers (Fluentbit, Alloy, etc.)
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: logs-writer
  namespace: default
spec:
  username: writer
  generatePassword: true
  targetRefs:
    - crd:
        kind: VLCluster/vlinsert
        name: example
        namespace: default
      paths:
        - "/insert/.*"
```

### VTCluster

`VTCluster` exposes two user-facing services — `vtinsert` (write path, port 10481) and `vtselect` (read path, port 10471).
Both can be routed through a single `VMAuth` ingress using `VMUser` objects.
See [VTCluster — Services and URLs](https://docs.victoriametrics.com/operator/resources/vtcluster/#services-and-urls) for the full list of service names and ports.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: traces-proxy
  namespace: default
spec:
  selectAllByDefault: true
  ingress:
    class_name: nginx                      # change to your ingress class
    host: victoriatraces.example.org
  unauthorizedUserAccessSpec:
    targetRefs:
      - crd:
          kind: VTCluster/vtselect
          name: example
          namespace: default
        paths:
          - "/select/.*"
---
# Write access for OpenTelemetry collectors
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: traces-writer
  namespace: default
spec:
  username: writer
  generatePassword: true
  targetRefs:
    - crd:
        kind: VTCluster/vtinsert
        name: example
        namespace: default
      paths:
        - "/insert/.*"
```

## Basic auth for targets

To authenticate a `VMServiceScrape`s over a metrics endpoint use [`basicAuth`](https://docs.victoriametrics.com/operator/api/#basicauth):

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  labels:
    k8s-apps: basic-auth-example
  name: basic-auth-example
spec:
  endpoints:
  - basicAuth:
      password:
        name: basic-auth
        key: password
      username:
        name: basic-auth
        key: user
    port: metrics
  selector:
    matchLabels:
      app: myapp

---

apiVersion: v1
kind: Secret
metadata:
  name: basic-auth
data:
  password: dG9vcg== # toor
  user: YWRtaW4= # admin
type: Opaque
```

## Create VMUser using an existing secret

### Bearer Token
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: victoria-reader-password # Name of the secret
  namespace: vm # Ensure this matches the namespace of your VMUser
type: Opaque
data:
  token: dmljdG9yaWEtaXMtY29vbA== # Base64 encoded value of 'victoria-is-cool'
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: victoria-reader
spec:
  name: victoria-reader
  bearerTokenSecret:
    name: victoria-reader-token
    key: token
  targetRefs:
    - crd:
        kind: VMCluster/vmselect
        name: victoriametrics-cluster
        namespace: vm
      target_path_suffix: '/select/1'
      paths:
        - '/prometheus/.*'
---
```
### Username and Password
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: victoria-reader-token # Name of the secret
  namespace: vm # Ensure this matches the namespace of your VMUser
type: Opaque
data:
  password: dmljdG9yaWEtaXMtY29vbA== # Base64 encoded value of 'victoria-is-cool'
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: victoria-reader-basic
spec:
  name: victoria-reader-basic
  username: victoria-reader
  passwordRef:
    name: victoria-reader-password
    key: password
  targetRefs:
    - crd:
        kind: VMCluster/vmselect
        name: victoriametrics-cluster
        namespace: vm
      target_path_suffix: '/select/1'
      paths:
        - '/prometheus/.*'
```

## Unauthorized access

You can expose some routes without authorization with `unauthorizedUserAccessSpec`.

Check more details in [VMAuth docs -> Unauthorized access](https://docs.victoriametrics.com/operator/resources/vmauth/#unauthorized-access).

More details about features of `VMAuth` and `VMUser` you can read in:
- [VMAuth docs](https://docs.victoriametrics.com/operator/resources/vmauth/),
- [VMUser docs](https://docs.victoriametrics.com/operator/resources/vmuser/).
