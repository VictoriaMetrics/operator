
# Victoria Metrics Operator

Operator serves to make running Victoria Metrics applications on top of Kubernetes as easy as possible while preserving Kubernetes-native configuration options.

## Installing operator

Obtain release from releases page:
[https://github.com/VictoriaMetrics/operator/releases](https://github.com/VictoriaMetrics/operator/releases)

```bash
wget https://github.com/VictoriaMetrics/operator/releases/download/v0.1.0/bundle_crd.zip
unzip  bundle_crd.zip 
```

First of all, you  have to create [custom resource definitions](https://github.com/VictoriaMetrics/operator)
```bash
kubectl apply -f release/crds
```
 
Then you need RBAC for operator, relevant configuration for the release can be found at release/operator/rbac.yaml

Change configuration for operator at `release/operator/manager.yaml`, possible settings: [operator-settings](/vars.MD)
and apply it:
```bash
kubectl apply -f release/operator/
```

Check the status of operator

```bash
kubectl get pods

#NAME                           READY   STATUS    RESTARTS   AGE
#vm-operator-667dfbff55-cbvkf   1/1     Running   0          101s

```

## Create related resources

The Victoria Metrics Operator introduces additional resources in Kubernetes to declare the desired state of a Victoria Metrics applications and Alertmanager cluster as well as the Prometheus resources configuration. The resources it introduces are:

* [VMSingle](#VMSingle)
* [VMCluter](#VMCluster)
* [VMAgent](#VMAgent)
* [VMAlert](#VMAlert)
* [Alertmanager](#Alertmanager)
* [VMServiceScrape](#VMServiceScrape)
* `VMRule`
* `VMPodScrape`


## VMSingle

[VMSingle](https://github.com/VictoriaMetrics/VictoriaMetrics/) represents database for storing metrics, for all possible config options check api [doc](/docs/api.MD#VMSingle):
 
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example-vmsingle-persisted
spec:
  retentionPeriod: "1"
  removePvcAfterDelete: true
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 1Gi      
EOF
```

Configuration can be extended with extraFlags and extraEnv, check flag documentation at [doc](https://github.com/VictoriaMetrics/VictoriaMetrics/blob/master/docs/Single-server-VictoriaMetrics.md)
for instance:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example-vmsingle-persisted
spec:
  retentionPeriod: "1"
  extraArgs:
    dedup.minScrapeInterval: 60s
EOF
```

Flag can be replaced with envVar, it's useful for retrieving value from secret:

```yaml
cat <<EOF | kubectl apply -f  -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example-vmsingle-persisted
spec:
  retentionPeriod: "1"
  extraEnvs:
    - name: DEDUP_MINSCRAPEINTERVAL
      valueFrom: 
        secretKeyRef:
           name: vm-secret
           key: dedup
EOF
```

## VMCluster

[VMCluster](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/cluster) represent a high-available and fault-tolerant version of VictoriaMetrics database.
For minimal version without persistent create simple custom resource definition:
 
```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-vmcluster
spec:
  # Add fields here
  retentionPeriod: "1"
  vmstorage:
    replicaCount: 2
  vmselect:
    replicaCount: 2
  vminsert:
    replicaCount: 2
EOF
```

For persistent it recommends to add persistent storage to `VMStorage` and `VMSelect`:

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
          storageClassName: standard
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
        cpu: "0.3"
        memory: "300Mi"
  vminsert:
    replicaCount: 2
EOF
```

You may also create cluster version with external VMStorage

```yaml
cat << EOF | kubectl apply -f -
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-vmcluster
spec:
  # Add fields here
  retentionPeriod: "1"
  vmselect:
    replicaCount: 2
    extraArgs:
       storageNode: "node-1:8401,node-2:8401"
  vminsert:
    replicaCount: 2
    extraArgs:
       storageNode: "node-1:8401,node-2:8401"
EOF
```

## VMAgent

[VMAgent](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmagent) -  is a tiny but brave agent, which helps you collect metrics from various sources and stores them in [VictoriaMetrics](https://github.com/VictoriaMetrics/VictoriaMetrics).
It requires access to Kubernetes API and you must create RBAC for it first, it can be found at `release/examples/VMAgent_rbac.yaml`

```bash
 kubectl apply -f release/examples/vmagent_rbac.yaml
```

Modify `VMAgent` config parameters at `release/examples/vmagent.yaml` and apply it, config options [doc](/docs/api.MD#VMAgent)

Example:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceMonitorNamespaceSelector: {}
  serviceMonitorSelector: {}
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle.default.svc:8429/api/v1/write"
EOF
```

## VMAlert

[VMAlert](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmalert) - executes a list of given [alerting](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/) or [recording](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/) rules against configured address. It
has few required config options - `datasource` and `notifierURL`, for other config parameters check [doc](/docs/api.MD#VMAlert)

```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  replicaCount: 1
  datasource:
    url: "http://vmagent-example-vmsingle.default.svc:8429"
  notifier:
      url: "http://alertmanager-operated:9093"
  evaluationInterval: "30s"
  ruleSelector:
    matchLabels: {}
EOF
```

## Alertmanager

Alertmanager - represents alertmanager configuration, first, you have to create secret with a configuration for it

```yaml
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: operator.vmalertmanager-example-alertmanager
stringData:
  alertmanager.yaml: |
    global:
      resolve_timeout: 5m
    route:
      group_by: ['job']
      group_wait: 30s
      group_interval: 5m
      repeat_interval: 12h
      receiver: 'webhook'
    receivers:
    - name: 'webhook'
      webhook_configs:
      - url: 'http://alertmanagerwh:30500/'
EOF
```

Then add `Alertmanager` object, other config options at [doc](/docs/api.MD#Alertmanager)

```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: example-alertmanager
spec:
  # Add fields here
  replicaCount: 1
EOF
```

## VMServiceScrape

Deploy three instances of a simple example application, which listens and exposes metrics on port `8080`.

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example-app
  template:
    metadata:
      labels:
        app: example-app
    spec:
      containers:
      - name: example-app
        image: fabxc/instrumented_app
        ports:
        - name: web
          containerPort: 8080
EOF
```

The VMServiceScrape has a label selector to select Services and their underlying Endpoint objects. The Service object for the example application selects the Pods by the `app` label having the `example-app` value. The Service object also specifies the port on which the metrics are exposed.

```yaml
cat <<EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: example-app
  labels:
    app: example-app
spec:
  selector:
    app: example-app
  ports:
  - name: web
    port: 8080
EOF
```

This Service object is discovered by a VMServiceScrape, which selects in the same way. The `app` label must have the value `example-app`.

```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: example-app
  labels:
    team: frontend
spec:
  selector:
    matchLabels:
      app: example-app
  endpoints:
  - port: web
EOF
```


## Migration from prometheus-operator objects

By default, the operator converts all existing prometheus-operator API objects into corresponding victoriametrics-operator objects

You can control this behaviour by setting env variable for operator:

```bash
#disable convertion for each object
VM_ENABLEDPROMETHEUSCONVERTER_PODMONITOR=false
VM_ENABLEDPROMETHEUSCONVERTER_SERVICESCRAPE=false
VM_ENABLEDPROMETHEUSCONVERTER_PROMETHEUSRULE=false
```

Otherwise, victoriametrics-operator would try to discover prometheus-operator API and convert it.


## Expose the VMSingle API


> WARNING: Pleas protect delete endpoint before exposing it [doc](https://github.com/VictoriaMetrics/VictoriaMetrics#how-to-delete-time-series)

Example for Kubernetes Nginx ingress [doc](https://kubernetes.github.io/ingress-nginx/examples/auth/basic/)

```bash
#generate creds
htpasswd -c auth foo

#create basic auth secret
cat <<EOF | kubectl apply -f -
apiVersion: v1
stringData:
  auth: foo:$apr1$wQ0fkxnl$KKOA/OqIZy48CuCB/A1Rc.
kind: Secret
metadata:
  name: vm-auth
type: Opaque
EOF

#create ingress rule
cat << EOF | kubectl apply -f -
apiVersion: networking/v1beta1                                                                                                                                                                                                                                                                                               
kind: Ingress                                                                                                                                                                                                                                                                                                                
metadata:                                                                                                                                                                                                                                                                                                                    
  annotations:                                                                                                                                                                                                                                                                                                               
    nginx.ingress.kubernetes.io/auth-realm: Authentication Required                                                                                                                                                                                                                                                          
    nginx.ingress.kubernetes.io/auth-secret: vm-auth                                                                                                                                                                                                                                                                         
    nginx.ingress.kubernetes.io/auth-type: basic
  labels:
    app: victoria-metrics-single
  name: victoria-metrics-single
spec:
  rules:
  - host: vmsingle.example.com
    http:
      paths:
      - backend:
          serviceName: vmsingle-example-vmsingle-persisted
          servicePort: 8428
        path: /
  tls:
  - hosts:
    - vmsingle.example.com
    secretName: ssl-name

EOF

#now you can access victoria metrics
curl -u foo:bar https://vmsingle.example/com/metrics
```
