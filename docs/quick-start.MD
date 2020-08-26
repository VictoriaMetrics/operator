
# Victoria Metrics Operator

Operator serves to make running Victoria Metrics applications on top of Kubernetes as easy as possible while preserving Kubernetes-native configuration options.

## Installing by Manifest

Obtain release from releases page:
[https://github.com/VictoriaMetrics/operator/releases](https://github.com/VictoriaMetrics/operator/releases)

```bash
wget https://github.com/VictoriaMetrics/operator/releases/download/v0.1.0/bundle_crd.zip
unzip  bundle_crd.zip 
```

> TIP, you can install operator to specific namespace with command
> sed -i "s/namespace: default/namespace: YOUR_NAMESPACE/g" release/operator/*

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


## Installing by Kustomize

You can install operator using [Kustomize](https://kubernetes-sigs.github.io/kustomize/) by pointing to the remote kustomization file.

```yaml
# Get latest release version from https://github.com/VictoriaMetrics/operator/releases/latest
export VM_VERSION=`basename $(curl -fs -o/dev/null -w %{redirect_url} https://github.com/VictoriaMetrics/operator/releases/latest)`

cat << EOF >> kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- github.com/VictoriaMetrics/operator/config/default?ref=${VM_VERSION}

namespace: monitoring
images:
- name: victoriametrics/operator
  newTag: ${VM_VERSION}
EOF
```


You can change [operator-settings](/vars.MD), or use your custom namespace see [kustomize-example](./kustomize-example).



Build template

```bash
kustomize build . -o monitoring.yaml
```

Apply manifests

```bash
kubectl apply -f monitoring.yaml
```


## Create related resources

The Victoria Metrics Operator introduces additional resources in Kubernetes to declare the desired state of a Victoria Metrics applications and Alertmanager cluster as well as the Prometheus resources configuration. The resources it introduces are:

* [VMSingle](#VMSingle)
* [VMCluter](#VMCluster)
* [VMAgent](#VMAgent)
* [VMAlert](#VMAlert)
* [Alertmanager](#Alertmanager)
* [VMServiceScrape](#VMServiceScrape)
* [VMRule](#VMRule)
* [VMPodScrape](#VMPodScrape)
* [VMProbe](#VMProbe)


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

For persistent it recommends adding persistent storage to `VMStorage` and `VMSelect`:

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
  serviceScrapeNamespaceSelector: {}
  podScrapeNamespaceSelector: {}
  podScrapeSelector: {}
  serviceScrapeSelector: {}
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
EOF
```

 `VMAgent` has four selectors specification for filtering `VMServiceScrape`, `VMProbe` and `VMPodScrape` objects:
 - `spec.serviceScrapeNamespaceSelector` - if it's nil, only serviceScrapes in the `VMAgent`s namespace can be selected.
    If it's defined with empty value: `spec.serviceScrapeNamespaceSelector: {}` - all namespaces are matched.
    You may also add matchLabels or matchExpression syntax, but your namespace must be tagged with specified labels first:
     ```bash
        kubectl label ns default name=default
     ``` 
      `spec.serviceScrapeNamespaceSelector:
               matchLabels:
                     name: default
      `
 - `spec.serviceScrapeSelector` - it selects `VMServiceScrape` objects by its labels. 
     If it's nil, but `spec.serviceScrapeNamespaceSelector` isn't, the last one will be used for selecting.
     If it's defined with empty value: `spec.serviceScrapeSelector: {}`, all objects can be selected depending on NamespaceSelector.

 - `spec.podScrapeNamespaceSelector` - if it's nil, only `PodScrape` in the `VMAgent`s namespace can be selected.
    If it's defined with empty value: `spec.podScrapeNamespaceSelector: {}` - all namespaces are matched.
    You may also add matchLabels or matchExpression syntax, but your namespace must be tagged with specified labels first:
     ```bash
        kubectl label ns default name=default
     ``` 
      `spec.podScrapeNamespaceSelector:
               matchLabels:
                     name: default
      `
 - `spec.podScrapeSelector` - it selects `VMPodScrape` objects by its labels. 
     If it's nil, but `spec.podScrapeNamespaceSelector` isn't, the last one will be used for selecting.
     If it's defined with empty value: `spec.podScrapeSelector: {}`, all objects can be selected depending on NamespaceSelector.

 - `spec.probeNamespaceSelector` - if it's nil, only `VMProbe` in the `VMAgent`s namespace can be selected.
    If it's defined with empty value: `spec.probeNamespaceSelector: {}` - all namespaces are matched.
    You may also add matchLabels or matchExpression syntax, but your namespace must be tagged with specified labels first:
     ```bash
        kubectl label ns default name=default
     ``` 
      `spec.probeNamespaceSelector:
               matchLabels:
                     name: default
      `
 - `spec.ProbeSelector` - it selects `VMProbe` objects by its labels. 
     If it's nil, but `spec.probeNamespaceSelector` isn't, the last one will be used for selecting.
     If it's defined with empty value: `spec.vmProbeSelector: {}`, all objects can be selected depending on NamespaceSelector.

     
this configuration will match only namespace with  the label - name=default and all `VMServiceScrape` objects at this namespace will be 
selected: 
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceScrapeSelector: {}
  serviceScrapeNamespaceSelector: 
      matchLabels:
         name: default
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
EOF
```

This configuration will select `VMServiceScrape` and `VMPodScrape` with labels - team=managers,project=cloud at any namespace:
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceScrapeNamespaceSelector: {}
  serviceScrapeSelector:
   matchLabels:
       team: managers
       project: cloud
  podScrapeSelector:
   matchLabels:
       team: managers
       project: clo
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
EOF
```

The last one will select any `VMServiceScrape`,`VMProbe` and `VMPodScrape` at `VMAgent` namespace:
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceScrapeSelector: {}
  podScrapeSelector: {}
  probSelector: {}
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
EOF 
```

## Alertmanager

Alertmanager - represents alertmanager configuration, first, you have to create secret with a configuration for it

```yaml
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: alertmanager-config
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
you have to set configSecret with name of secret, that we created before - `alertmanager-config`.
```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanager
metadata:
  name: example-alertmanager
spec:
  # Add fields here
  replicaCount: 1
  configSecret: alertmanager-config
EOF
```


## VMAlert

[VMAlert](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmalert) - executes a list of given [alerting](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/) or [recording](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/) rules against configured address. It
has few required config options - `datasource` and `notifier are required`, for other config parameters check [doc](/docs/api.MD#VMAlert).

```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  replicaCount: 1
  datasource:
    url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429"
  notifier:
      url: "http://vmalertmanager-example-alertmanager.default.svc:9093"
  evaluationInterval: "30s"
  ruleNamespaceSelector: {}
  ruleSelector: {}
EOF
```

 `VMAlert` has exactly the same logic for filtering `VMRule` with `spec.ruleNamespaceSelector` and `spec.ruleSelector` 
 as `VMAgent` does:
  - `spec.ruleNamespaceSelector` - if its nil, only `VMRule`s in the `VMalert` namespace can be selected.
     If it's defined with empty value: `spec.ruleNamespaceSelector: {}` - all namespaces are matched.
     You may also add matchLabels or matchExpression syntax, but your namespace must be tagged with specified labels first: 
       `spec.ruleNamespaceSelector:
                matchLabels:
                      name: default
       `
  - `spec.ruleSelector` - it selects `VMRule` objects by its labels. 
      If it's nil, but `spec.ruleNamespaceSelector` isn't, the last one will be used for selecting.
      If it's defined with empty value: `spec.ruleSelector: {}`, all objects depending on NamespaceSelector.

Selecting any `VMRule` at `VMAlert` namespace:
```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  replicaCount: 1
  datasource:
    url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429"
  notifier:
      url: "http://alertmanager-operated:9093"
  evaluationInterval: "30s"
  ruleSelector: {}
EOF
```

Select `VMRule`s matching labels - severity=major,notify=all at namespaces with label - env=production:
```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  replicaCount: 1
  datasource:
    url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429"
  notifier:
      url: "http://alertmanager-operated:9093"
  evaluationInterval: "30s"
  ruleNamespaceSelector: 
    matchLabels:
       env: production
  ruleSelector:
    matchLabels:
     severity: major
     notify: all
EOF
```


## VMServiceScrape

 It generates part of `VMAgent` configuration with `Endpoint` kubernetes_sd role for service discovery targets
 by corresponding `Service` and it's `Endpoint`s.
 It has various options for scraping configuration of target (with basic auth,tls access, by specific port name etc).

Let's make some demo, you have to deploy [VMAgent](#VMAgent) and [VMSingle](#VMSingle) from previous step with match any selectors:

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
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: example-vmagent
spec:
  serviceScrapeNamespaceSelector: {}
  podScrapeNamespaceSelector: {}
  podScrapeSelector: {}
  serviceScrapeSelector: {}
  replicaCount: 1
  serviceAccountName: vmagent
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
EOF
```



 Then deploy three instances of a simple example application, which listens and exposes metrics on port `8080`.

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

 Check status for pods:
 ```bash
kubectl get pods 
NAME                                                   READY   STATUS    RESTARTS   AGE
example-app-594f97677c-g72v8                           1/1     Running   0          23s
example-app-594f97677c-kn66h                           1/1     Running   0          20s
example-app-594f97677c-l8xfd                           1/1     Running   0          17s
vm-operator-646df47888-jvfvj                           1/1     Running   0          63m
vmagent-example-vmagent-5777fdf7bf-tctcv               2/2     Running   1          34m
vmalertmanager-example-alertmanager-0                  2/2     Running   0          11m
vmsingle-example-vmsingle-persisted-794b59ccc6-fnkpt   1/1     Running   0          36m

```

Checking logs for `VMAgent`:
```bash
kubectl logs vmagent-example-vmagent-5777fdf7bf-tctcv vmagent 
2020-08-02T18:18:17.226Z        info    VictoriaMetrics/app/vmagent/remotewrite/remotewrite.go:98       Successfully reloaded relabel configs
2020-08-02T18:18:17.229Z        info    VictoriaMetrics/lib/promscrape/scraper.go:137   found changes in "/etc/vmagent/config_out/vmagent.env.yaml"; applying these changes
2020-08-02T18:18:17.289Z        info    VictoriaMetrics/lib/promscrape/scraper.go:303   kubernetes_sd_configs: added targets: 2, removed targets: 0; total targets: 2
```
 By default, operator creates `VMServiceScrape` for its components, so we have 2 targets - `VMAgent` and `VMSingle`.
 
Let's add monitoring to our application:

First we add the `Service` for application:
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

`VMServiceScrape` has a label selector to select `Services` and their underlying Endpoint objects.
`Service` object for the example application selects the Pods by the `app` label having the `example-app` value. 
`Service` object also specifies the port on which the metrics exposed.

The final part, add `VMServiceScrape`:

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
 The `VMServiceScrape` has no  labels, but it will be selected by `VMAgent`, because we are launched `VMAgent` with empty selector.

 Let's check `VMAgent` logs (you have to wait some time for config sync, usually its around 1 min):
 
 ```bash
kubectl logs vmagent-example-vmagent-5777fdf7bf-tctcv vmagent --tail 100
2020-08-03T08:24:13.312Z	info	VictoriaMetrics/lib/promscrape/scraper.go:106	SIGHUP received; reloading Prometheus configs from "/etc/vmagent/config_out/vmagent.env.yaml"
2020-08-03T08:24:13.312Z	info	VictoriaMetrics/app/vmagent/remotewrite/remotewrite.go:98	Successfully reloaded relabel configs
2020-08-03T08:24:13.315Z	info	VictoriaMetrics/lib/promscrape/scraper.go:137	found changes in "/etc/vmagent/config_out/vmagent.env.yaml"; applying these changes
2020-08-03T08:24:13.418Z	info	VictoriaMetrics/lib/promscrape/scraper.go:303	kubernetes_sd_configs: added targets: 3, removed targets: 0; total targets: 5

```

3 new targets added by a config reload.

## VMPodScrape

 It generates config part of `VMAgent` with  kubernetes_sd role  `pod`, that would match all `pods` having specific labels and ports. 
 It has various options for scraping configuration of target (with basic auth,tls access, by specific port name etc).
 
  Add `VMAgent` and Example app from step above and continue  this step.
  
  Let's add 2 new pods:
  ```yaml
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: example-pod
    owner: dev
    env: test
  name: example-app-pod-0
spec:
  containers:
  - image: fabxc/instrumented_app
    imagePullPolicy: IfNotPresent
    name: example-app
    ports:
    - containerPort: 8080
      name: web
      protocol: TCP
    resources: {}
---
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: example-pod-scrape
    owner: dev
    env: dev
  name: example-app-pod-1
spec:
  containers:
  - image: fabxc/instrumented_app
    imagePullPolicy: IfNotPresent
    name: example-app
    ports:
    - containerPort: 8080
      name: web
      protocol: TCP
    resources: {}
EOF
```

 Ensure, that pods started:
 ```bash
kubectl get pods
NAME                                                   READY   STATUS    RESTARTS   AGE
example-app-594f97677c-g72v8                           1/1     Running   0          3m40s
example-app-594f97677c-kn66h                           1/1     Running   0          3m37s
example-app-594f97677c-l8xfd                           1/1     Running   0          3m34s
example-app-pod-0                                      1/1     Running   0          14s
example-app-pod-1                                      1/1     Running   0          14s
vm-operator-646df47888-jvfvj                           1/1     Running   0          67m
vmagent-example-vmagent-5777fdf7bf-tctcv               2/2     Running   1          37m
vmalertmanager-example-alertmanager-0                  2/2     Running   0          14m
vmsingle-example-vmsingle-persisted-794b59ccc6-fnkpt   1/1     Running   0          40m
```

 These pods have the same label `owner: dev` and port named `web`, so we can select them by `VMPodScrape` with matchLabels expression:
```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMPodScrape
metadata:
  name: example-pod-scrape
spec:
  podMetricsEndpoints:
    - port: web
      scheme: http
  selector:
    matchLabels:
     owner: dev
EOF
```  

 Lets check `VMAgent` logs:
 ```bash
kubectl logs vmagent-example-vmagent-5777fdf7bf-tctcv vmagent --tail 100
2020-08-03T08:51:13.582Z	info	VictoriaMetrics/app/vmagent/remotewrite/remotewrite.go:98	Successfully reloaded relabel configs
2020-08-03T08:51:13.585Z	info	VictoriaMetrics/lib/promscrape/scraper.go:137	found changes in "/etc/vmagent/config_out/vmagent.env.yaml"; applying these changes
2020-08-03T08:51:13.652Z	info	VictoriaMetrics/lib/promscrape/scraper.go:303	kubernetes_sd_configs: added targets: 2, removed targets: 0; total targets: 7
```
 
2 new target was added.

## VMRule

It generates `VMAlert` config with ruleset defined at `VMRule` spec.

Lets create `VMAlert` with selector for `VMRule` with label project=devops.
  You also need datasource from previous step [VMSingle](#VMSingle) and [VMAgent](#VMAgent) connected to it.

```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlert
metadata:
  name: example-vmalert
spec:
  replicaCount: 1
  datasource:
    url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429"
  notifier:
      url: "http://vmalertmanager-example-alertmanager.default.svc:9093"
  evaluationInterval: "30s"
  ruleSelector:
    matchLabels:
      project: devops
EOF
```

Ensure, that it started and ready:
```bash
kubectl get pods -l app.kubernetes.io/name=vmalert
NAME                                       READY   STATUS    RESTARTS   AGE
vmalert-example-vmalert-6f8748c6f9-hcfrr   2/2     Running   0          2m26s

kubectl logs vmalert-example-vmalert-6f8748c6f9-hcfrr vmalert
2020-08-03T08:52:21.990Z        info    VictoriaMetrics/app/vmalert/manager.go:83       reading rules configuration file from "/etc/vmalert/config/vm-example-vmalert-rulefiles-0/*.yaml"
2020-08-03T08:52:21.992Z        info    VictoriaMetrics/app/vmalert/group.go:153        group "vmAlertGroup" started with interval 30s
2020-08-03T08:52:21.992Z        info    VictoriaMetrics/lib/httpserver/httpserver.go:76 starting http server at http://:8080/
2020-08-03T08:52:21.993Z        info    VictoriaMetrics/lib/httpserver/httpserver.go:77 pprof handlers are exposed at http://:8080/debug/pprof/

```
`VMAlert` shipped with default rule `vmAlertGroup`

Let's add simple rule for `VMAlert` itself, `delta(vmalert_config_last_reload_errors_total[5m]) > 1`

```yaml
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: example-vmrule-reload-config
  labels:
      project: devops
spec:
  groups:
    - name: vmalert
      rules:
        - alert: vmalert config reload error
          expr: delta(vmalert_config_last_reload_errors_total[5m]) > 0
          for: 10s
          labels:
            severity: major
            job:  "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'error reloading vmalert config, reload count for 5 min {{ $value }}'
EOF
```

 Ensure, that new alert was started:
 ```bash
kubectl logs vmalert-example-vmalert-6f8748c6f9-hcfrr vmalert 
2020-08-03T09:07:49.772Z	info	VictoriaMetrics/app/vmalert/web.go:45	api config reload was called, sending sighup
2020-08-03T09:07:49.772Z	info	VictoriaMetrics/app/vmalert/main.go:115	SIGHUP received. Going to reload rules ["/etc/vmalert/config/vm-example-vmalert-rulefiles-0/*.yaml"] ...
2020-08-03T09:07:49.772Z	info	VictoriaMetrics/app/vmalert/manager.go:83	reading rules configuration file from "/etc/vmalert/config/vm-example-vmalert-rulefiles-0/*.yaml"
2020-08-03T09:07:49.773Z	info	VictoriaMetrics/app/vmalert/group.go:169	group "vmAlertGroup": received stop signal
2020-08-03T09:07:49.773Z	info	VictoriaMetrics/app/vmalert/main.go:124	Rules reloaded successfully from ["/etc/vmalert/config/vm-example-vmalert-rulefiles-0/*.yaml"]
2020-08-03T09:07:49.773Z	info	VictoriaMetrics/app/vmalert/group.go:153	group "vmalert" started with interval 30s

```

 Let's trigger it by adding some incorrect rule
 
```yaml
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: example-vmrule-incorrect-rule
  labels:
      project: devops
spec:
  groups:
    - name: incorrect rule
      rules:
        - alert: vmalert bad config
          expr: bad expression
          for: 10s
          labels:
            severity: major
          annotations:
            value: "{{ $badValue | bad function }}"
EOF
```

`VMAlert` will report incorrect rule config and fire alert:
```bash
2020-08-03T09:11:40.672Z	info	VictoriaMetrics/app/vmalert/main.go:115	SIGHUP received. Going to reload rules ["/etc/vmalert/config/vm-example-vmalert-rulefiles-0/*.yaml"] ...
2020-08-03T09:11:40.672Z	info	VictoriaMetrics/app/vmalert/manager.go:83	reading rules configuration file from "/etc/vmalert/config/vm-example-vmalert-rulefiles-0/*.yaml"
2020-08-03T09:11:40.673Z	error	VictoriaMetrics/app/vmalert/main.go:119	error while reloading rules: cannot parse configuration file: invalid group "incorrect rule" in file "/etc/vmalert/config/vm-example-vmalert-rulefiles-0/default-example-vmrule-incorrect-rule.yaml": invalid rule "incorrect rule"."vmalert bad config": invalid expression: unparsed data left: "expression"
```

Clean up incorrect rule:
```bash
kubectl delete vmrule example-vmrule-incorrect-rule
```

## VMProbe

 `VMProbe` required `VMAgent` and some external prober, blackbox exporter in our case. Ensure that you have `VMAgent` and `VMSingle`:
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
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
   name: example-vmagent
spec:
   serviceScrapeNamespaceSelector: {}
   podScrapeNamespaceSelector: {}
   probeNamespaceSelector: {}
   podScrapeSelector: {}
   serviceScrapeSelector: {}
   probeSelector: {}
   replicaCount: 1
   serviceAccountName: vmagent
   remoteWrite:
     - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
EOF
 ```

 Then add `BlackBox` exporter with simple config:
 
```yaml
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-blackbox-exporter
  labels:
    app: prometheus-blackbox-exporter
data:
  blackbox.yaml: |
    modules:
      http_2xx:
        http:
          preferred_ip_protocol: ip4
          valid_http_versions:
          - HTTP/1.1
          - HTTP/2
          valid_status_codes: []
        prober: http
        timeout: 5s
---
kind: Service
apiVersion: v1
metadata:
  name: prometheus-blackbox-exporter
  labels:
    app: prometheus-blackbox-exporter
spec:
  type: ClusterIP
  ports:
    - name: http
      port: 9115
      protocol: TCP
  selector:
    app: prometheus-blackbox-exporter

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus-blackbox-exporter
  labels:
    app: prometheus-blackbox-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prometheus-blackbox-exporter
  template:
    metadata:
      labels:
        app: prometheus-blackbox-exporter
    spec:
      containers:
        - name: blackbox-exporter
          image: "prom/blackbox-exporter:v0.17.0"
          args:
            - "--config.file=/config/blackbox.yaml"
          resources:
            {}
          ports:
            - containerPort: 9115
              name: http
          livenessProbe:
            httpGet:
              path: /health
              port: http
          readinessProbe:
            httpGet:
              path: /health
              port: http
          volumeMounts:
            - mountPath: /config
              name: config
      volumes:
        - name: config
          configMap:
            name: prometheus-blackbox-exporter
EOF
```

Ensure, that pods are ready:

```bash
kubectl get pods
NAME                                                   READY   STATUS    RESTARTS   AGE
prometheus-blackbox-exporter-5b5f44bd9c-2szdj          1/1     Running   0          3m3s
vmagent-example-vmagent-84886cd4d9-x8ml6               2/2     Running   1          9m22s
vmsingle-example-vmsingle-persisted-8584486b68-mqg6b   1/1     Running   0          11m
```

Now define some `VMProbe`, lets start with basic static target and probe `VMAgent` with its service address, for accessing
blackbox exporter, you have to specify its url at `VMProbe` config. Lets get both services names:
```bash
kubectl get svc
NAME                                  TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)    AGE
kubernetes                            ClusterIP   10.96.0.1       <none>        443/TCP    4h21m
prometheus-blackbox-exporter          ClusterIP   10.105.251.80   <none>        9115/TCP   4m36s
vmagent-example-vmagent               ClusterIP   10.102.31.47    <none>        8429/TCP   12m
vmsingle-example-vmsingle-persisted   ClusterIP   10.107.69.7     <none>        8429/TCP   12m
```

So, we will probe `VMAgent` with url - `vmagent-example-vmagent.default.svc:9115/heath` with blackbox url: 
`prometheus-blackbox-exporter.default.svc:9115` and module: `http_2xx` it was specified at blackbox configmap.

```yaml
cat << EOF | kubectl apply -f -
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMProbe
metadata:
  name: probe-agent
spec:
  jobName: static-probe
  vmProberSpec:
     # by default scheme http, and path is /probe
     url: prometheus-blackbox-exporter.default.svc:9115
  module: http_2xx
  targets:
   staticConfig: 
      targets:
      -  vmagent-example-vmagent.default.svc:8429/health
  interval: 2s 
EOF
```

Now new target must be added to `VMAgent` configuration, and it starts probing itself throw blackbox exporter.

Let's try another target probe type - `Ingress`. Create ingress rule for `VMSingle` and create `VMProbe` for it:

```yaml

cat << EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1beta1                                                                                                                                                                                                                                                                                             
kind: Ingress                                                                                                                                                                                                                                                                                                                
metadata:                                                                                                                                                                                                                                                                                                                    
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
  - host: vmsingle2.example.com
    http:
      paths:
      - backend:
          serviceName: vmsingle-example-vmsingle-persisted
          servicePort: 8428
        path: /

---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMProbe
metadata:
  name: probe-single-ingress
spec:
  vmProberSpec:
     # by default scheme http, and path is /probe
     url: prometheus-blackbox-exporter.default.svc:9115
  module: http_2xx
  targets:
   ingress: 
      selector:
       matchLabels:
        app: victoria-metrics-single
  interval: 10s 
EOF
```

This configuration will add 2 additional targets for probing: `vmsingle2.example.com` and `vmsingle.example.com`.

But probes will be unsuccessful, coz there is no such hosts.

## Migration from prometheus-operator objects

By default, the operator converts all existing prometheus-operator API objects into corresponding victoriametrics-operator objects

You can control this behaviour by setting env variable for operator:

```bash
#disable convertion for each object
VM_ENABLEDPROMETHEUSCONVERTER_PODMONITOR=false
VM_ENABLEDPROMETHEUSCONVERTER_SERVICESCRAPE=false
VM_ENABLEDPROMETHEUSCONVERTER_PROMETHEUSRULE=false
VM_ENABLEDPROMETHEUSCONVERTER_PROBE=false
```
Otherwise, victoriametrics-operator would try to discover prometheus-operator API and convert it.


 Conversion of api objects can be controlled by annotations, added to `VMObject`s, there are following annotations:
 - `operator.victoriametrics.com/merge-meta-strategy` - it controls syncing of metadata labels and annotations between
    `VMObject`s and `Prometheus` api objects during updates to `Prometheus` objects. By default, it has `prefer-prometheus`.
     And annotations and labels will be used from `Prometheus` objects, manually set values will be dropped.
    You can set it to `prefer-victoriametrics`. In this case all labels and annotations applied to `Prometheus` object
     will be ignored and `VMObject` will use own values.
     Two additional strategies annotations -`merge-victoriametrics-priority` and `merge-prometheus-priority` merges labelSets into one combined labelSet, with priority.
    Example:
```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  annotations:
    meta.helm.sh/release-name: prometheus
    operator.victoriametrics.com/merge-meta-strategy: prefer-victoriametrics
  labels:
    release: prometheus
  name: prometheus-monitor
spec:
  endpoints: []
```

- `operator.victoriametrics.com/ignore-prometheus-updates` - it controls updates from Prometheus api objects.
   By default, it set to `disabled`. You define it to `enabled` state and all updates from Prometheus api objects will be
   ignored.
```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  annotations:
    meta.helm.sh/release-name: prometheus
    operator.victoriametrics.com/ignore-prometheus-updates: enabled
  labels:
    release: prometheus
  name: prometheus-monitor
spec:
  endpoints: []
```



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
apiVersion: networking.k8s.io/v1beta1                                                                                                                                                                                                                                                                                          
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
