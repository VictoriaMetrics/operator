---
weight: 3
title: Istio
menu:
  docs:
    parent: operator-integrations
    weight: 3
---


In this guide, we'll walk through deploying and using VictoriaMetrics with [Istio](https://istio.io/) [service mesh](https://en.wikipedia.org/wiki/Service_mesh).
Istio is a tool that helps manage network traffic between your applications in Kubernetes.
We'll start with a simple setup and gradually add more advanced configurations,
covering common challenges and best practices along the way.
We recommend using the minimal viable configuration that meets your needs to avoid unnecessary complexity.

## Prerequisites

Before we dive into the details, let's set up a test Kubernetes cluster with VictoriaMetrics and Istio.
This setup will be the foundation for the rest of the tutorial, so make sure to follow each step carefully.
We'll install Istio first without enabling the mesh network. Later, we'll enable it in permissive mode and then strict mode,
showing how to adapt VictoriaMetrics to work with each setting.
Then, we proceed by installing a basic VictoriaMetrics setup, as outlined in the [Quick Start](https://docs.victoriametrics.com/operator/quick-start/) documentation.

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

Install Istio:
```sh
curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.26.0 sh -;
./istio-1.26.0/bin/istioctl install -f ./istio-1.26.0/samples/bookinfo/demo-profile-no-gateways.yaml -y;

# Output:
# ...
# ✔ Istio core installed ⛵️                                                                                                                                                                                           
# ✔ Istiod installed 🧠                                                                                                                                                                                               
# ✔ Installation complete  
```
These commands download and install Istio service mesh with a basic demo profile configuration.

Now, install:
- VictoriaMetrics [operator](https://docs.victoriametrics.com/operator/quick-start/#operator)
- VictoriaMetrics [storage](https://docs.victoriametrics.com/operator/quick-start/#storage)
- VictoriaMetrics [agent and demo-app](https://docs.victoriametrics.com/operator/quick-start/#scraping)

## Ambient mode

In [ambient mode](https://istio.io/latest/docs/ambient/overview/), Istio handles network traffic management using a proxy on each node (Layer 4) 
and optionally another proxy for each namespace (Layer 7).
You don't need to make any changes to your applications or VictoriaMetrics pods if you're using ambient mode. 
So, you can skip the next sections as they solely related to [sidecar mode](https://istio.io/latest/docs/setup/).

## Permissive policy

Everything should work without any changes as long as both 
the application and the VictoriaMetrics pods are using a permissive [peer authentication policy](https://istio.io/latest/docs/tasks/security/authentication/authn-policy/).
In this mode, Istio proxy sidecar will attempt to secure traffic when possible. 
However, scraping requests from VMAgent will still be sent as plain text.

Here we just check that everything works as expected.
If you followed the [prerequisites](https://docs.victoriametrics.com/operator/integrations/istio#prerequisites) steps, 
VictoriaMetrics and Istio should already be installed and running in your Kubernetes cluster.
However, at this point, traffic is not yet flowing through the service mesh. 
To enable the mesh, create a PeerAuthentication policy and ensure Istio sidecar injection is enabled in the namespaces `default` and `vm`:

Create the policy:
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
The `default` policy in the `istio-system` namespace is unique because it applies mesh-wide to all services.
It configures the authentication mode as `PERMISSIVE`, enabling both unencrypted and encrypted traffic.

Run the following commands to enable Istio sidecar injection:
```sh
kubectl label namespace default istio-injection=enabled;
kubectl label namespace vm istio-injection=enabled;

# Output:
# namespace/default labeled
# namespace/vm labeled
```
Applying the `istio-injection=enabled` label signals Istio to inject the sidecar proxy into every pod launched in these namespaces. 
This allows your workloads to seamlessly join the service mesh without modifying pod specifications.

Re-deploy services:
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
# ...
# deployment "vm-operator" successfully rolled out
# ...
# deployment "vmsingle-demo" successfully rolled out
# ...
# deployment "vmagent-demo" successfully rolled out
# ...
# deployment "demo-app" successfully rolled out
```

To verify that Istio sidecars have been properly injected into the pods, run:
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
At this point, pods in the `vm` and `default` namespaces have the istio-proxy injected.
Pods in other namespaces do not.

Let's check `VMAgent` istio-proxy logs to verify that it is scraping metrics from the demo app:
```sh
DEMO_APP_POD=$(kubectl get pod -n default -l "app.kubernetes.io/name=demo-app" -o jsonpath="{.items[0].metadata.name}");
DEMO_APP_POD_IP=$(kubectl get pod -n default ${DEMO_APP_POD} -o jsonpath='{.status.podIP}');
VMAGENT_POD_NAME=$(kubectl get pod -n vm -l "app.kubernetes.io/name=vmagent" -o jsonpath="{.items[0].metadata.name}");

kubectl -n vm logs -f "${VMAGENT_POD_NAME}" -c istio-proxy 2>&1 | grep "${DEMO_APP_POD_IP}";

# Output:
# [2025-05-25T11:46:50.187Z] "GET /metrics HTTP/1.1" 200 - via_upstream - "-" 0 43 2 1 "-" "vm_promscrape" "c5e5b566-d179-9e29-b02f-2c20382ae353" "10.244.0.13:8080" "10.244.0.13:8080" PassthroughCluster 10.244.0.12:57118 10.244.0.13:8080 10.244.0.12:57114 - allow_any
```
You can also check out the `VMAgent` UI by following the steps outlined in the [Quick Start - Scraping](https://docs.victoriametrics.com/operator/quick-start/#scraping) section.

## Partially strict policy

By default, `VMAgent` sends scrape requests directly to pod IP:PORT.
Istio doesn’t support `STRICT` mode for direct pod-to-pod traffic.
Instead, Istio expects traffic to go through Kubernetes Services to ensure security and [identity checks](https://istio.io/latest/docs/concepts/security/#istio-identity).

Still, we can enable `STRICT` mode across the cluster, except for the `vm` namespace.
We’ll also configure `VMAgent` to act like an Istio sidecar and use Istio certificates to encrypt traffic.
Even though `VMAgent` won’t verify the destination’s certificates (so it’s not full mTLS),
it will still encrypt the traffic and let you collect metrics from your apps.
This setup handles 99% of use cases and should work for most setups.

To do this, set `STRICT` mode for all namespaces, but keep the `vm` namespace in `PERMISSIVE` mode:
```sh
cat <<'EOF' > global-peer-authentication.yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: STRICT
EOF

cat <<'EOF' > vm-ns-peer-authentication.yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: demo
  namespace: vm
spec:
  mtls:
    mode: PERMISSIVE
EOF

kubectl -n istio-system apply -f global-peer-authentication.yaml;
kubectl -n vm apply -f vm-ns-peer-authentication.yaml;

# Output:
# peerauthentication.security.istio.io/default configured
# peerauthentication.security.istio.io/demo created
```

At this point, scraping metrics from `demo-app` will stop working.
The Istio sidecar in `demo-app` will block plain HTTP requests from `VMAgent` with a 503 error:
```sh
DEMO_APP_POD=$(kubectl get pod -n default -l "app.kubernetes.io/name=demo-app" -o jsonpath="{.items[0].metadata.name}");
DEMO_APP_POD_IP=$(kubectl get pod -n default ${DEMO_APP_POD} -o jsonpath='{.status.podIP}');
VMAGENT_POD_NAME=$(kubectl get pod -n vm -l "app.kubernetes.io/name=vmagent" -o jsonpath="{.items[0].metadata.name}");

kubectl -n vm logs -f "${VMAGENT_POD_NAME}" -c istio-proxy 2>&1 | grep "${DEMO_APP_POD_IP}";

# Output:
#[2025-05-25T11:50:20.173Z] "GET /metrics HTTP/1.1" 503 UC upstream_reset_before_response_started{connection_termination} - "-" 0 95 1 - "-" "vm_promscrape" "bd84c1bc-67c2-98fa-b0bf-1da879935bf0" "10.244.0.13:8080" "10.244.0.13:8080" PassthroughCluster 10.244.0.12:47602 10.244.0.13:8080 10.244.0.12:57114 - allow_any
```

To fix this, we’ll mount Istio-provided certificates into `VMAgent`.
We’ll create a Kustomize patch to update the `VMAgent` deployment with the needed certificate settings.
In the end, you’ll still apply `vmagent-demo.yaml` like usual, so you can also just edit the file directly:
```sh
mkdir -p vmagent-with-istio-certs;

cat <<'EOF' > vmagent-with-istio-certs/patch.yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAgent
metadata:
  name: demo
  namespace: vm
spec:
  podMetadata:
    annotations:
      proxy.istio.io/config: |  # configure an env variable `OUTPUT_CERTS` to write certificates to the given folder
        proxyMetadata:
          OUTPUT_CERTS: /etc/istio-certs
      sidecar.istio.io/userVolumeMount: '[{"name": "istio-certs", "mountPath": "/etc/istio-certs"}]'
  volumeMounts:
    - mountPath: /etc/istio-certs/
      name: istio-certs
      readOnly: true
  volumes:
    - emptyDir:
        medium: Memory
      name: istio-certs
EOF

cat <<'EOF' > vmagent-with-istio-certs/kustomization.yaml
resources:
  - ../vmagent-demo.yaml

patches:
  - path: patch.yaml
    target:
      kind: VMAgent
      name: demo
EOF

kustomize build vmagent-with-istio-certs -o vmagent-demo.yaml --load-restrictor=LoadRestrictionsNone;
cat vmagent-demo.yaml;

# Output:
# apiVersion: operator.victoriametrics.com/v1beta1
# kind: VMAgent
# ...
```

Now, deploy the updated `VMAgent`:
```sh
kubectl -n vm apply -f vmagent-demo.yaml;
kubectl -n vm wait --for=jsonpath='{.status.updateStatus}'=operational vmagent/demo;
kubectl -n vm rollout status deployment vmagent-demo  --watch=true;

# Output:
# vmagent.operator.victoriametrics.com/demo configured
# vmagent.operator.victoriametrics.com/demo condition met
# deployment "vmagent-demo" successfully rolled out
```

Next, update the demo-app `VMServiceScrape` to use the Istio certificates:
```sh

mkdir -p demo-app-scrape-with-tls;

cat <<'EOF' > demo-app-scrape-with-tls/patch.yaml
- op: replace
  path: /spec/endpoints/0/scheme
  value: https
- op: add
  path: /spec/endpoints/0/tlsConfig
  value:
    caFile: /etc/istio-certs/root-cert.pem
    certFile: /etc/istio-certs/cert-chain.pem
    insecureSkipVerify: true
    keyFile: /etc/istio-certs/key.pem

EOF

cat <<'EOF' > demo-app-scrape-with-tls/kustomization.yaml
resources:
  - ../demo-app-scrape.yaml

patches:
  - path: patch.yaml
    target:
      kind: VMServiceScrape
      name: demo-app-service-scrape
EOF

kustomize build demo-app-scrape-with-tls -o demo-app-scrape.yaml --load-restrictor=LoadRestrictionsNone;
cat demo-app-scrape.yaml;

# Output:
# apiVersion: operator.victoriametrics.com/v1beta1
# kind: VMServiceScrape
# ...
```

At this point, everything should be working again.
Metrics from `demo-app` are now scraped over HTTPS using Istio certificates.
Metrics from `vmagent-demo` and `vmsingle-demo` are still scraped over plain HTTP,
because they run in `PERMISSIVE` mode.
Remote writes use mTLS, since `VMAgent` sends data to the `vmsingle` service.

Be sure to update all VMServiceScrape resources that target pods running in `STRICT` mode.
[Be sure not to use tlsConfig](https://istio.io/latest/docs/ops/integrations/prometheus/#tls-settings) for resources that run in `PERMISSIVE` mode.

<!-- TODO: ## Strict policy -->
<!-- TODO: ## Scrape Istio metrics -->
