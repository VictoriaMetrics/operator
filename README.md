# VictoriaMetrics operator

## quick-start


1) create crds

```bash
kubectl apply -f deploy/crds
```

2) deploy operator
```bash

kubectl apply -f deploy/rbac.yaml
#
kubectl apply -f deploy/operator.yaml

```
3) create victoriametrics database - vmsingle resource
```yaml

kubectl apply -f deploy/examples/vmsingle.yaml

```
4) create rbac for victoriametrics agent 
```yaml

kubectl apply -f deploy/examples/vmagent_rbac.yaml

kubectl apply -f deploy/examples/vmagent.yaml

```
5) create alertmanager

```yaml

kubectl create secret generic alertmanager-example-alertmanager --from-file deploy/examples/alertmanager.yaml
kubectl apply -f deploy/examples/alertmanager-sts.yaml


```

6) create victoria metrics alert
```bash
kubectl apply -f deploy/examples/vmalert.yaml

```
7) check status of created resources
```bash
kubectl get pods

NAME                                         READY   STATUS    RESTARTS   AGE
prometheus-example-alertmanager-0            2/2     Running   0          82s
vm-operator-86b4d677f9-dx72f                 1/1     Running   2          6m15s
vmagent-example-vmagent-68d5786b99-gsf6m     2/2     Running   1          4m35s
vmalert-example-vmalert-6449b87864-jf2f9     2/2     Running   0          48s
vmsingle-example-vmsingle-548cccbd8f-g8292   1/1     Running   0          5m18s


```





## examples


## todo
1) rbac - now it`s too broad probably
3) tests
4) documentation


## Advanced

### sidecar containers

### vm agent HA


## limitations

- Alertmanager is supported at api monitoring.victoriametrics.com/v1beta1     
- alert relabel is not supported

## development

- operator-sdk verson v0.17.0 +  [https://github.com/operator-framework/operator-sdk]
- golang 1.13 +
- minikube 

start:
```bash
make run
```




https://book.kubebuilder.io/reference/markers/crd-validation.html