---
weight: 1
title: Istio
menu:
  docs:
    parent: operator-integrations
    weight: 3
---

https://istio.io/

In this guide, we'll walk through deploying and using VictoriaMetrics with Istio service mesh.
Istio is a tool that helps manage network traffic between your applications in Kubernetes.
We'll start with a simple setup and gradually add more advanced configurations,
covering common challenges and best practices along the way.

<!-- TODO: You'll also learn how to configure VictoriaMetrics to scrape metrics exposed by Istio. -->

## Prerequisites

https://docs.victoriametrics.com/operator/quick-start/

Before we dive into the details, let's set up a test Kubernetes cluster with VictoriaMetrics and Istio.
This setup will be the foundation for the rest of the tutorial, so make sure to follow each step carefully.
We'll install Istio first without enabling the mesh network. Later, we'll enable it in permissive mode and then strict mode,
showing how to adapt VictoriaMetrics to work with each setting.
For more details on VMSingle, VMAgent, and demo-app, check out the quick-start guide.

Create a test Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/):
```sh
kind create cluster --name=istio;

# Output:
# ...
# Set kubectl context to "kind-istio"
# You can now use your cluster with:
#
# kubectl cluster-info --context kind-istio
```

Install VictoriaMetrics operator:
```sh
export VM_OPERATOR_VERSION=$(basename $(curl -fs -o /dev/null -w %{redirect_url} \
  https://github.com/VictoriaMetrics/operator/releases/latest));

# TODO: rm when the latest is not v0.58.0 (it has buggy vmagent).
VM_OPERATOR_VERSION=v0.57.0

wget -O operator-and-crds.yaml \
  "https://github.com/VictoriaMetrics/operator/releases/download/$VM_OPERATOR_VERSION/install-no-webhook.yaml";

kubectl apply -f operator-and-crds.yaml;
kubectl -n vm rollout status deployment vm-operator --watch=true;

# Output:
# namespace/vm configured
# ...
# deployment "vm-operator" successfully rolled out
```

Install metrics storage `VMSingle`:
```sh
cat <<'EOF' > vmsingle-demo.yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: demo
  namespace: vm
EOF

kubectl -n vm apply -f vmsingle-demo.yaml;
kubectl -n vm wait --for=jsonpath='{.status.updateStatus}'=operational vmsingle/demo;
kubectl -n vm rollout status deployment vmsingle-demo  --watch=true;

# Output:
# vmsingle.operator.victoriametrics.com/demo created
# vmsingle.operator.victoriametrics.com/demo condition met
# deployment "vmsingle-demo" successfully rolled out
```

Install scraping agent `VMAgent`:
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
kubectl -n vm wait --for=jsonpath='{.status.updateStatus}'=operational vmagent/demo;
kubectl -n vm rollout status deployment vmagent-demo  --watch=true;

# Output:
# vmagent.operator.victoriametrics.com/demo created
# vmagent.operator.victoriametrics.com/demo condition met
# deployment "vmsingle-demo" successfully rolled out
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
kubectl -n default rollout status deployment demo-app  --watch=true;

# Output:
# deployment.apps/demo-app created
# service/demo-app created
# Waiting for deployment "demo-app" rollout to finish: 0 of 1 updated replicas are available...
# deployment "demo-app" successfully rolled out
```
This creates a simple application that will generate metrics for us to collect and monitor.

Tell VMAgent to scrape demo app metrics:
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
kubectl -n default wait --for=jsonpath='{.status.updateStatus}'=operational vmservicescrapes/demo-app-service-scrape;

# Output:
# vmservicescrape.operator.victoriametrics.com/demo-app-service-scrape created
# vmservicescrape.operator.victoriametrics.com/demo-app-service-scrape condition met
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
These commands download and install Istio service mesh with a basic demo profile configuration.

## Ambient mode

[ambient mode](https://istio.io/latest/docs/ambient/overview/)
[sidecar mode](https://istio.io/latest/docs/setup/)

In ambient mode, Istio handles network traffic management using a proxy on each node (Layer 4) and optionally another proxy for each namespace (Layer 7).
If you're using ambient mode, you don't need to make any changes to your applications or VictoriaMetrics pods. 
You can skip the next sections.

However, most clusters use sidecar mode, so continue reading the next sections if that's your case.

## Permissive policy

https://istio.io/latest/docs/tasks/security/authentication/authn-policy/
[prerequisites](#Prerequisites)

When both the application and the VictoriaMetrics pod are running with Istio's permissive authentication policy, 
everything should work without changes. 
The Istio sidecar will try to secure traffic where possible, but scraping requests from VMAgent that use plain HTTP
will still be allowed to pass through in permissive mode.

If you followed the prerequisites steps, you should have VictoriaMetrics and Istio installed and running in your Kubernetes cluster.
However, the traffic is not yet part of the service mesh.
To include traffic in the mesh with permissive mode, we need to create a peer authentication policy and
select which namespaces should have the istio-proxy sidecar injected. Let's do that now.  

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
This policy called default in the istio-system namespace is special - it applies to all services in the mesh. 
It sets the authentication mode to PERMISSIVE, which allows both plain and encrypted traffic.

Enable Istio injection in `default` and `vm` namespaces:
```sh
kubectl label namespace default istio-injection=enabled;
kubectl label namespace vm istio-injection=enabled;

# Output:
# namespace/default labeled
# namespace/vm labeled
```
These commands add the istio-injection=enabled label to our namespaces, 
which tells Istio to automatically inject the sidecar proxy into any pods created in these namespaces.

Re-deploy VictoriaMetrics service and demo-app so the Istio sidecar is properly injected:
```sh
kubectl -n vm rollout restart deployment vm-operator;
kubectl -n vm rollout status deployment vm-operator --watch=true;

kubectl -n vm rollout restart deployment vmsingle-demo;
kubectl -n vm rollout status deployment vmsingle-demo  --watch=true;

kubectl -n vm rollout restart deployment vmagent-demo; 
kubectl -n vm rollout status deployment vmagent-demo  --watch=true;

kubectl -n default rollout restart deployment demo-app; 
kubectl -n default rollout status deployment demo-app  --watch=true;

# Output:
# deployment.apps/vm-operator restarted
# Waiting for deployment "vm-operator" rollout to finish: 0 out of 1 new replicas have been updated...
# Waiting for deployment "vm-operator" rollout to finish: 1 old replicas are pending termination...
# Waiting for deployment "vm-operator" rollout to finish: 1 old replicas are pending termination...
# deployment "vm-operator" successfully rolled out
# deployment.apps/vmsingle-demo restarted
# Waiting for deployment "vmsingle-demo" rollout to finish: 0 out of 1 new replicas have been updated...
# Waiting for deployment "vmsingle-demo" rollout to finish: 0 out of 1 new replicas have been updated...
# Waiting for deployment "vmsingle-demo" rollout to finish: 0 out of 1 new replicas have been updated...
# Waiting for deployment "vmsingle-demo" rollout to finish: 0 of 1 updated replicas are available...
# deployment "vmsingle-demo" successfully rolled out
# deployment.apps/vmagent-demo restarted
# Waiting for deployment "vmagent-demo" rollout to finish: 1 old replicas are pending termination...
# Waiting for deployment "vmagent-demo" rollout to finish: 1 old replicas are pending termination...
# deployment "vmagent-demo" successfully rolled out
# deployment.apps/demo-app restarted
# Waiting for deployment "demo-app" rollout to finish: 0 out of 1 new replicas have been updated...
# Waiting for deployment "demo-app" rollout to finish: 1 old replicas are pending termination...
# Waiting for deployment "demo-app" rollout to finish: 1 old replicas are pending termination...
# deployment "demo-app" successfully rolled out
```

To check that istio sidecar containers are correctly injected into the pods, run:
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
Pods in the vm and default namespaces now have an istio-proxy container injected because we added the istio-injection=enabled label to these namespaces.
Other namespaces don't have this label, so their pods don't have the istio-proxy container.

Let's check that scraping is still working.
First, let's port-forward the VMAgent service.
Then we'll open the VMAgent targets UI to check that metrics are being scraped correctly (wait a minute or two for changes to take effect).
```sh
VMAGENT_POD_NAME=$(kubectl get pod -n vm -l "app.kubernetes.io/name=vmagent" -o jsonpath="{.items[0].metadata.name}");

kubectl port-forward -n vm $VMAGENT_POD_NAME 8429:8429;
# Output:
# Forwarding from 127.0.0.1:8429 -> 8429
# Forwarding from [::1]:8429 -> 8429
```
Open http://localhost:8429/targets in your browser. You should see three targets in the UP state for demo-app, vmsingle, and vmagent.

## Partially strict policy

Next step could be enforcing strict mTLS policy for all traffic except for `vm` namespace and configure 


permissive -> strict error
unexpected status code returned when scraping "http://10.244.0.7:8080/metrics": 503; expecting 200; response body: "upstream connect error or disconnect/reset before headers. reset reason: connection termination"

https://spiffe.io

Strict policy works reliably for traffic going through Kubernetes Services, since Istio can map pod SPIFFE URIs (secure identities) to pod IPs.

However, VMAgent does not use services when scraping metrics. 
For performance reasons, it connects directly to pod IPs and ports, bypassing Kubernetes Services entirely.
As a result, the VMAgent sidecar cannot validate the destination's certificate, 
since it has no knowledge of the expected SPIFFE URI for the target pod. 
In fact, it cannot even guarantee that the destination IP belongs to a pod at all.
So the sidecar either drops the request or pass through it unauthenticated (in this case destination sidecar would reject it 503)

In this section, we present a workaround that maintains partial security while enabling VMAgent to scrape metrics.

The strategy is:
- Run everything in strict mTLS mode, except the vm namespace, which should run in permissive mode.
- Allow VMAgent to scrape using TLS with Istio-provided certificates.
- Disable certificate verification on the VMAgent side to avoid SPIFFE mismatch issues.
- Set the VMAgent sidecar to passthrough mode for all outbound traffic.

This configuration does not provide full mTLS security. Since VMAgent doesn’t verify the destination certificate.
However, the receiving side (the pod being scraped) can still enforce strict mTLS for all incoming traffic.

## Strict policy

## Scrape Istio metrics


