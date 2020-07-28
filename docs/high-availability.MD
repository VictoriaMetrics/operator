
# High Availability

High availability is not only important for customer facing software, but if the monitoring infrastructure is not highly available, then there is a risk that operations people are not notified for alerts of the customer facing software. Therefore high availability must be just as thought through for the monitoring stack, as for anything else.

## VMAgent

To run VMAgent in a highly available manner you have to configure deduplication at Victoria Metrics first [doc](https://github.com/VictoriaMetrics/VictoriaMetrics/blob/master/docs/Single-server-VictoriaMetrics.md#deduplication)

Then increase replicas for VMAgent.

## VMAlert

It can be launched with multiple replicas without additional configuration, alertmanager is responsible for alert deduplication.


## VMAlertmanager
The final step of the high availability scheme is Alertmanager, when an alert triggers, actually fires alerts against *all* instances of an Alertmanager cluster. 

The Alertmanager, starting with the `v0.5.0` release, ships with a high availability mode. It implements a gossip protocol to synchronize instances of an Alertmanager cluster regarding notifications that have been sent out, to prevent duplicate notifications. It is an AP (available and partition tolerant) system. Being an AP system, means that notifications are guaranteed to be sent at least once. 

The Victoria Metrics Operator ensures that Alertmanager clusters are properly configured to run highly available on Kubernetes.


## VMSingle

 It doesnt support high availability, for such purpose use VmCluster. It will be introduced later.


## VMCluster

 Cluster version provides fullset of high availability features - metrics replication, node failover, horizontal scaling.
 
 For using cluster version you have to create corresponding crd object:
 ```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-vmcluster-persistent
spec:
  # Add fields here
  retentionPeriod: "4"
  replicationFactor: 2
  vmstorage:
    replicaCount: 2
    storageDataPath: "/vm-data"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 10Gi
    resources:
      limits:
        cpu: "0.5"
        memory: 500Mi
  vmselect:
    replicaCount: 2
    cacheMountPath: "/select-cache"
    storage:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 2Gi
    resources:
      limits:
        cpu: "0.2"
        memory: "300Mi"
  vminsert:
    replicaCount: 2
EOF
```
 
  Then wait for cluster become ready
  ```bash
get vmclusters -w
NAME                           INSERT COUNT   STORAGE COUNT   SELECT COUNT   AGE   STATUS
example-vmcluster-persistent   2              2               2              2s    expanding
example-vmcluster-persistent   2              2               2              30s   operational
```

 Get links for connection by executing command:
 ```bash
kubectl get svc -l app.kubernetes.io/instance=example-vmcluster-persistent
NAME                                     TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)                      AGE
vminsert-example-vmcluster-persistent    ClusterIP   10.96.34.94   <none>        8480/TCP                     69s
vmselect-example-vmcluster-persistent    ClusterIP   None          <none>        8481/TCP                     79s
vmstorage-example-vmcluster-persistent   ClusterIP   None          <none>        8482/TCP,8400/TCP,8401/TCP   85s

```

 Now you can connect vmagent to vminsert and vmalert to vmselect
 !NOTE do not forget to create rbac for vmagent
 ```yaml
cat << EOF | kubectl apply -f  -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceMonitorNamespaceSelector: {}
  serviceMonitorSelector: {}
  # Add fields here
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vminsert-example-vmcluster-persistent.default.svc.cluster.local:8480/insert/0/prometheus/api/v1/write"
EOF
```

Lets add vmalert 

```yaml
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