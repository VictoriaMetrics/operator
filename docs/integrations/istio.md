---
weight: 1
title: Istio
menu:
  docs:
    parent: operator-integrations
    weight: 3
---

In this guide, we’ll walk through deploying and using VictoriaMetrics in an environment running the [Istio](https://istio.io/) service mesh. 
We'll start with a simple setup and gradually introduce more advanced configurations, 
covering edge cases and best practices along the way.
By the end of this guide, you’ll also learn how to configure VictoriaMetrics to scrape metrics exposed by Istio.

## Prerequisites

Before we dive into the details, let’s set up locally a Kubernetes cluster with Istio and VictoriaMetrics.
This setup will serve as the foundation for the rest of the tutorial, so make sure to follow each step carefully.

{{% collapse name="Prepare demo cluster" %}}

We will use [Kind](https://kind.sigs.k8s.io/) and [Docker](https://www.docker.com/) to create a local Kubernetes cluster:
```sh
kind create cluster --name=istio;

# Output:
# ...
# Set kubectl context to "kind-istio"
# You can now use your cluster with:
#
# kubectl cluster-info --context kind-istio
```

Install Istio:
```sh
curl -L https://istio.io/downloadIstio | sh -;
./istio-1.26.0/bin/istioctl install -f ./istio-1.26.0/samples/bookinfo/demo-profile-no-gateways.yaml -y;

# Output:
# ...
# ✔ Istio core installed ⛵️                                                                                                                                                                                           
# ✔ Istiod installed 🧠                                                                                                                                                                                               
# ✔ Installation complete  
```

Set global permissive policy:
```sh
cat <<'EOF' > global-peer-authentication.yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: PERMISSIVE
EOF
kubectl -n istio-system apply -f global-peer-authentication.yaml;

# Output:
# peerauthentication.security.istio.io/default created
```

Enable Istio injection in `default` and `vm` namespaces:
```sh
kubectl label namespace default istio-injection=enabled;
kubectl create namespace vm || true;
kubectl label namespace vm istio-injection=enabled;

# Output:
# namespace/default labeled
# namespace/vm created
# namespace/vm labeled
```

Follow these steps to install the VictoriaMetrics Operator along with the CRDs, 
and to deploy a `VMSingle` and `VMAgent` instance. 
For more details, refer to the [quick-start](https://docs.victoriametrics.com/operator/quick-start/) guide.

Install operator:
```sh
export VM_OPERATOR_VERSION=$(basename $(curl -fs -o /dev/null -w %{redirect_url} \
  https://github.com/VictoriaMetrics/operator/releases/latest));

# TODO: rm when the latest is not v0.58.0 (it has buggy vmagent).
VM_OPERATOR_VERSION=v0.57.0

wget -O operator-and-crds.yaml \
  "https://github.com/VictoriaMetrics/operator/releases/download/$VM_OPERATOR_VERSION/install-no-webhook.yaml";
kubectl apply -f operator-and-crds.yaml;

# Output:
# namespace/vm configured
# ...
```

Install `VMSingle`:
```sh
cat <<'EOF' > vmsingle-demo.yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: demo
  namespace: vm
EOF

kubectl -n vm apply -f vmsingle-demo.yaml;

# Output:
# vmsingle.operator.victoriametrics.com/demo created
```

Install `VMAgent`:
```sh
cat <<'EOF' > vmagent-demo.yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: demo
  namespace: vm
spec:
  selectAllByDefault: true
  remoteWrite:
    - url: "http://vmsingle-demo.vm.svc:8429/api/v1/write"
EOF

kubectl -n vm apply -f vmagent-demo.yaml;

# Output:
# vmagent.operator.victoriametrics.com/demo created
```

Install [demo-app](https://github.com/VictoriaMetrics/demo-app):
```sh
cat <<'EOF' > demo-app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
  namespace: default
  labels:
    app.kubernetes.io/name: demo-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: demo-app
  template:
    metadata:
      labels:
        app.kubernetes.io/name: demo-app
    spec:
      containers:
        - name: main
          image: docker.io/victoriametrics/demo-app:1.2
---
apiVersion: v1
kind: Service
metadata:
  name: demo-app
  namespace: default
  labels:
    app.kubernetes.io/name: demo-app
spec:
  selector:
    app.kubernetes.io/name: demo-app
  ports:
    - port: 8080
      name: http
EOF

kubectl -n default apply -f demo-app.yaml;

# Output:
# deployment.apps/demo-app created
# service/demo-app created
```

Scrape demo app metrics:
```sh
cat <<'EOF' > demo-app-scrape.yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: demo-app-service-scrape
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: demo-app
  endpoints:
  - port: http
    path: /metrics
EOF

kubectl apply -f demo-app-scrape.yaml;

# Output:
# vmservicescrape.operator.victoriametrics.com/demo-app-service-scrape created
```

Run check that istio sidecar containers are injected into the pods:
```sh
kubectl get pods --all-namespaces  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{range .spec.containers[*]}- {.name}{"\n"}{end}{"\n"}{end}'

# Output:
# demo-app-5b65b4d975-h47g6
# - main
# - istio-proxy
#
# istiod-7f898458c5-pq878
# - discovery
#
# ...
# vmsingle-demo-7f5dd5cfd9-pspj2
# - vmsingle
# - istio-proxy
```
Pods in `vm` and `default` namespaces have `istio-proxy` container injected because of namespace `istio-injection=enabled` label.
Other namespaces do not have this label, so pods in those namespaces do not have `istio-proxy` container injected.

{{% /collapse %}}

## Ambient mode

In [ambient mode](https://istio.io/latest/docs/ambient/overview/), Istio implements its features using a per-node Layer 4 (L4) proxy, and optionally a per-namespace Layer 7 (L7) proxy.
In this mode no modifications are required to application or VictoriaMetrics pods.

## Permissive policy

https://istio.io/latest/docs/tasks/security/authentication/authn-policy/

If both the application and the VictoriaMetrics pod are operating under Istio permissive authentication policy, 
everything should function correctly without any modifications.
The Istio sidecar will attempt to secure traffic where possible; however, scraping requests from `VMAgent` are sent directly to the pod over plain HTTP. 
Istio will allow these requests to pass through without blocking them.

Let's open `VMAgent` targets UI to check that metrics are scraped correctly.

First, port forward the `VMAgent` service:
```sh
VMAGENT_POD_NAME=$(kubectl get pod -n vm -l "app.kubernetes.io/name=vmagent" -o jsonpath="{.items[0].metadata.name}");

kubectl port-forward -n vm $VMAGENT_POD_NAME 8429:8429;
# Output:
# Forwarding from 127.0.0.1:8429 -> 8429
# Forwarding from [::1]:8429 -> 8429
```

Open http://localhost:8429/targets. You should see there three targets in `UP` state for `demo-app`, `vmsingle` and `vmagent`.

## Hybrid policies

## Strict policy

## Scrape Istio metrics





permissive -> strict error
unexpected status code returned when scraping "http://10.244.0.7:8080/metrics": 503; expecting 200; response body: "upstream connect error or disconnect/reset before headers. reset reason: connection termination"