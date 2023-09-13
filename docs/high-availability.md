---
sort: 8
weight: 8
title: High Availability
---

# High Availability

High availability is not only important for customer-facing software but if the monitoring infrastructure is not highly available, then there is a risk that operations people are not notified of alerts.
Therefore, high availability must be just as thought through for the monitoring stack, as for anything else.

<!-- HA for operator -->
<!-- TODO: need to be actualized (and possibly moved to separate resources docs) -->


## VMCluster

The cluster version provides a full set of high availability features - metrics replication, node failover, horizontal scaling.

For using the cluster version you have to create the corresponding CRD object:

```console
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-vmcluster-persistent
spec:
  retentionPeriod: "4"
  replicationFactor: 2       
  vmstorage:
    replicaCount: 2
    storageDataPath: "/vm-data"
    podMetadata:
      labels:
        owner: infra
    affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: "app.kubernetes.io/name"
                operator: In
                values:
                - "vmstorage"
            topologyKey: "kubernetes.io/hostname"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 10Gi
    resources:
      limits:
        cpu: "2"
        memory: 2048Mi
  vmselect:
    replicaCount: 2
    cacheMountPath: "/select-cache"
    podMetadata:
      labels:
        owner: infra
    affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: "app.kubernetes.io/name"
                operator: In
                values:
                - "vmselect"
            topologyKey: "kubernetes.io/hostname"

    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 2Gi
    resources:
      limits:
        cpu: "1"
        memory: "500Mi"
  vminsert:
    replicaCount: 2
    podMetadata:
      labels:
        owner: infra
    affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: "app.kubernetes.io/name"
                operator: In
                values:
                - "vminsert"
            topologyKey: "kubernetes.io/hostname"
    resources:
      limits:
        cpu: "1"
        memory: "500Mi"

EOF
```

Then wait for the cluster becomes ready

```console
kubectl get vmclusters -w
NAME                           INSERT COUNT   STORAGE COUNT   SELECT COUNT   AGE   STATUS
example-vmcluster-persistent   2              2               2              2s    expanding
example-vmcluster-persistent   2              2               2              30s   operational
```

Get links for connection by executing the command:

```console
kubectl get svc -l app.kubernetes.io/instance=example-vmcluster-persistent
NAME                                     TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)                      AGE
vminsert-example-vmcluster-persistent    ClusterIP   10.96.34.94   <none>        8480/TCP                     69s
vmselect-example-vmcluster-persistent    ClusterIP   None          <none>        8481/TCP                     79s
vmstorage-example-vmcluster-persistent   ClusterIP   None          <none>        8482/TCP,8400/TCP,8401/TCP   85s
```

Now you can connect vmagent to vminsert and vmalert to vmselect

>NOTE do not forget to create rbac for vmagent

```console
cat << EOF | kubectl apply -f  -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceScrapeNamespaceSelector: {}
  serviceScrapeSelector: {}
  podScrapeNamespaceSelector: {}
  podScrapeSelector: {}
  # Add fields here
  replicaCount: 1
  remoteWrite:
    - url: "http://vminsert-example-vmcluster-persistent.default.svc.cluster.local:8480/insert/0/prometheus/api/v1/write"
EOF
```

Config for vmalert

```console
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  # Add fields here
  replicas: 1
  datasource:
    url: "http://vmselect-example-vmcluster-persistent.default.svc.cluster.local:8481/select/0/prometheus"
  notifier:
    url: "http://alertmanager-operated.default.svc:9093"
  evaluationInterval: "10s"
  ruleSelector: {}
EOF
```

