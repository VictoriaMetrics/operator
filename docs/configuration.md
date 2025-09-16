---
weight: 4
title: Configuration
menu:
  docs:
    parent: "operator"
    weight: 4
aliases:
  - /operator/configuration/
  - /operator/configuration/index.html
tags:
  - kubernetes
  - logs
  - metrics
---

The operator is set up using [environment variables](https://docs.victoriametrics.com/operator/configuration/#environment-variables) and [command-line flags](https://docs.victoriametrics.com/operator/configuration/#flags).
Most environment variables control settings related to [Resources](https://docs.victoriametrics.com/operator/resources/), like CPU and memory defaults, image versions.
Command-line flags configure the operator itself, like leader election, TLS, webhook validation, and rate limits.

## Environment variables

Run this command {{% available_from "v0.57.0" %}} to see all environment variables your operator supports:
```sh 
OPERATOR_POD_NAME=$(kubectl get pod -l "app.kubernetes.io/name=victoria-metrics-operator"  -n vm -o jsonpath="{.items[0].metadata.name}");
kubectl exec -n vm "$OPERATOR_POD_NAME" -- /app --printDefaults 2>&1

# Output:
# KEY                   DEFAULT        REQUIRED    DESCRIPTION
# VM_METRICS_VERSION    v1.117.0       false       
# VM_LOGS_VERSION       v1.21.0        false 
# ... 
```

This is the latest operator environment variables:
{{% content "env.md" %}}

## Modify environment variables

To add environment variables to the operator, use the following [Kustomize](https://kustomize.io/)-based approach. 
This method assumes the operator was installed using the [Quick Start guide](https://docs.victoriametrics.com/operator/quick-start/#operator).
Alternatively, you can edit the manifest file directly. 
If you used Helm, apply the changes using Helm’s values configuration.

The example below customize CPU\Memory default limits for `VMSingle` resource.
The commands create a patch `add-operator-envs/patch.yaml` that adds environment variables to the operator deployment, 
a `add-operator-envs/kustomization.yaml` configuration to apply the patch, and then call `kustomize build` to rewrite the `operator-and-crds.yaml` file with the applied changes:
```sh
mkdir -p add-operator-envs;

cat <<'EOF' > add-operator-envs/patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vm-operator
  namespace: vm
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM
          value: "3000Mi"
        - name: VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU
          value: "2400m"
EOF

cat <<'EOF' > add-operator-envs/kustomization.yaml
resources:
  - ../operator-and-crds.yaml

patches:
  - path: patch.yaml
    target:
      kind: Deployment
      name: vm-operator
EOF

kustomize build add-operator-envs -o operator-and-crds.yaml --load-restrictor=LoadRestrictionsNone;
cat operator-and-crds.yaml | grep -E -A 1 "VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM|VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU";

# Output:
#        - name: VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM
#          value: 3000Mi
#        - name: VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU
#          value: 2400m
```

Apply the changes to the operator deployment:
```
kubectl apply -f operator-and-crds.yaml;
kubectl -n vm rollout status deployment vm-operator --watch=true;

# Output:
# Waiting for deployment "vm-operator" rollout to finish: 1 old replicas are pending termination...
# Waiting for deployment "vm-operator" rollout to finish: 1 old replicas are pending termination...
# deployment "vm-operator" successfully rolled out
```

Run this command to print modified environment variables:
```sh
kubectl get deployment -n vm vm-operator \
    -o jsonpath='{range .spec.template.spec.containers[?(@.name=="manager")].env[*]}{.name}{"\n"}{end}';

# Output:
# WATCH_NAMESPACE
# VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM
# VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU
```

## Flags

Run this command to see all flags your operator supports:
```sh 
OPERATOR_POD_NAME=$(kubectl get pod -l "app.kubernetes.io/name=victoria-metrics-operator"  -n vm -o jsonpath="{.items[0].metadata.name}");
kubectl exec -n vm "$OPERATOR_POD_NAME" -- /app --help 2>&1;

# Output:
# Usage of /app:
#   -client.burst int
#       defines K8s client burst (default 100)
# ...
```

This is the latest operator flags:
{{% content "flags.md" %}}

## Modify flags

To add flags to the operator, use the following [Kustomize](https://kustomize.io/)-based approach.
This method assumes the operator was installed using the [Quick Start guide](https://docs.victoriametrics.com/operator/quick-start/#operator).
Alternatively, you can edit the manifest file directly.
If you used Helm, apply the changes using Helm’s values configuration.

The example below shows how to change log level.
The commands create a patch `add-operator-flag/patch.yaml` that adds command line argument to the operator deployment,
a `add-operator-flag/kustomization.yaml` configuration to apply the patch, and then call `kustomize build` to rewrite the `operator-and-crds.yaml` file with the applied changes:
```sh
mkdir -p add-operator-flag;

cat <<'EOF' > add-operator-flag/patch.yaml
- op: add
  path: /spec/template/spec/containers/0/args/-
  value: '-zap-log-level=debug'
EOF

cat <<'EOF' > add-operator-flag/kustomization.yaml
resources:
  - ../operator-and-crds.yaml

patches:
  - path: patch.yaml
    target:
      kind: Deployment
      name: vm-operator
EOF

kustomize build add-operator-flag -o operator-and-crds.yaml --load-restrictor=LoadRestrictionsNone;
cat operator-and-crds.yaml | grep "zap-log-level";

# Output:
#        - -zap-log-level=debug
```

Apply the changes to the operator deployment:
```
kubectl apply -f operator-and-crds.yaml;
kubectl -n vm rollout status deployment vm-operator --watch=true;

# Output:
# Waiting for deployment "vm-operator" rollout to finish: 1 old replicas are pending termination...
# Waiting for deployment "vm-operator" rollout to finish: 1 old replicas are pending termination...
# deployment "vm-operator" successfully rolled out
```

Run this command to print modified flags variables:
```sh
kubectl get deployment -n vm vm-operator \
  -o jsonpath='{range .spec.template.spec.containers[?(@.name=="manager")]}{.args[*]}{end}{"\n"}'

# Output:
# --leader-elect --health-probe-bind-address=:8081 --metrics-bind-address=:8080 -zap-log-level=debug
```

## Scrape operator metrics

To collect the operator metrics, you can create a [VMServiceScrape](https://docs.victoriametrics.com/operator/resources/vmservicescrape/) resource.
Configure it to collects metrics from the pods that match the operator labels. 
Apply scrape config in `vm` namespace, same where the operator is running.

The example below works if you installed the operator using the [Quick Start - Operator](https://docs.victoriametrics.com/operator/quick-start/#operator) section.
You may need to update the match labels and\or namespace to fit your own operator setup.

```sh
cat <<'EOF' > operator-scrape.yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMServiceScrape
metadata:
  name: operator-service-scrape
  namespace: vm
spec:
  selector:
    matchLabels:
      # You might need to change the labels below
      app.kubernetes.io/instance: default
      app.kubernetes.io/name: victoria-metrics-operator
  endpoints:
    - port: http
  # Uncomment the lines below if the VMServiceScrape is applied in  a namespace
  # different from the one where the operator is running.
  # namespaceSelector:
  #  matchNames:
  #    - default
EOF

kubectl apply -f operator-scrape.yaml;
kubectl wait -n vm --for=jsonpath='{.status.updateStatus}'=operational vmservicescrape/operator-service-scrape;

# Output:
# vmservicescrape.operator.victoriametrics.com/operator-service-scrape created
# vmservicescrape.operator.victoriametrics.com/operator-service-scrape condition met
```

You can check if the operator metrics are collected correctly by using the vmagent UI.
Note, It may take a minute or two for vmagent to load the new scrape config and begin collecting the metrics.

You can find instructions for accessing the vmagent UI in the [Quick Start - Scraping](https://docs.victoriametrics.com/operator/quick-start/#scraping) section.

## Conversion of prometheus-operator objects

You can read detailed instructions about configuring prometheus-objects conversion in [this document](https://docs.victoriametrics.com/operator/integrations/prometheus/).

## Helm-charts

In [Helm charts](https://docs.victoriametrics.com/helm/) some important configuration parameters are implemented as separate flags in `values.yaml`:

### victoria-metrics-k8s-stack

For possible values refer to [parameters](https://docs.victoriametrics.com/helm/victoria-metrics-k8s-stack/#parameters).

Also, checkout [here possible ENV variables](https://docs.victoriametrics.com/operator/configuration/#environment-variables) to configure operator behaviour.
ENV variables can be set in the `victoria-metrics-operator.env` section.

```yaml
# values.yaml

victoria-metrics-operator:
  image:
    # -- Image repository
    repository: victoriametrics/operator
    # -- Image tag
    tag: v0.35.0
    # -- Image pull policy
    pullPolicy: IfNotPresent

  # -- Tells helm to remove CRD after chart remove
  cleanupCRD: true
  cleanupImage:
    repository: gcr.io/google_containers/hyperkube
    tag: v1.18.0
    pullPolicy: IfNotPresent

  operator:
    # -- By default, operator converts prometheus-operator objects.
    disable_prometheus_converter: false
    # -- Compare-options and sync-options for prometheus objects converted by operator for properly use with ArgoCD
    prometheus_converter_add_argocd_ignore_annotations: false
    # -- Enables ownership reference for converted prometheus-operator objects,
    # it will remove corresponding victoria-metrics objects in case of deletion prometheus one.
    enable_converter_ownership: false
    # -- By default, operator creates psp for its objects.
    psp_auto_creation_enabled: true
    # -- Enables custom config-reloader, bundled with operator.
    # It should reduce  vmagent and vmauth config sync-time and make it predictable.
    useCustomConfigReloader: false

  # -- extra settings for the operator deployment. full list Ref: https://docs.victoriametrics.com/operator/vars
  env:
    # -- default version for vmsingle
    - name: VM_VMSINGLEDEFAULT_VERSION
      value: v1.43.0
    # -- container registry name prefix, e.g. docker.io
    - name: VM_CONTAINERREGISTRY
      value: ""
    # -- image for custom reloader (see the useCustomConfigReloader parameter)
    - name: VM_CUSTOMCONFIGRELOADERIMAGE
      value: victoriametrics/operator:config-reloader-v0.32.0

  # By default, the operator will watch all the namespaces
  # If you want to override this behavior, specify the namespace it needs to watch separated by a comma.
  # Ex: my_namespace1,my_namespace2
  watchNamespace: ""

  # Count of operator instances (can be increased for HA mode)
  replicaCount: 1

  # -- VM operator log level
  # -- possible values: info and error.
  logLevel: "info"

  # -- Resource object
  resources:
    {}
    # limits:
    #   cpu: 120m
    #   memory: 320Mi
    # requests:
    #   cpu: 80m
    #   memory: 120Mi
```

### victoria-metrics-operator

For possible values refer to [parameters](https://docs.victoriametrics.com/helm/victoria-metrics-operator/#parameters).

Also, checkout [here possible ENV variables](https://docs.victoriametrics.com/operator/configuration/#environment-variables) to configure operator behaviour.
ENV variables can be set in the `env` section.

```yaml
# values.yaml

image:
  # -- Image repository
  repository: victoriametrics/operator
  # -- Image tag
  tag: v0.35.0
  # -- Image pull policy
  pullPolicy: IfNotPresent

operator:
  # -- By default, operator converts prometheus-operator objects.
  disable_prometheus_converter: false
  # -- Compare-options and sync-options for prometheus objects converted by operator for properly use with ArgoCD
  prometheus_converter_add_argocd_ignore_annotations: false
  # -- Enables ownership reference for converted prometheus-operator objects,
  # it will remove corresponding victoria-metrics objects in case of deletion prometheus one.
  enable_converter_ownership: false
  # -- By default, operator creates psp for its objects.
  psp_auto_creation_enabled: true
  # -- Enables custom config-reloader, bundled with operator.
  # It should reduce  vmagent and vmauth config sync-time and make it predictable.
  useCustomConfigReloader: false

# -- extra settings for the operator deployment. full list Ref: https://docs.victoriametrics.com/operator/vars
env:
  # -- default version for vmsingle
  - name: VM_VMSINGLEDEFAULT_VERSION
    value: v1.43.0
  # -- container registry name prefix, e.g. docker.io
  - name: VM_CONTAINERREGISTRY
    value: ""
  # -- image for custom reloader (see the useCustomConfigReloader parameter)
  - name: VM_CUSTOMCONFIGRELOADERIMAGE
    value: victoriametrics/operator:config-reloader-v0.32.0

# By default, the operator will watch all the namespaces
# If you want to override this behavior, specify the namespace it needs to watch separated by a comma.
# Ex: my_namespace1,my_namespace2
watchNamespace: ""

# Count of operator instances (can be increased for HA mode)
replicaCount: 1

# -- VM operator log level
# -- possible values: info and error.
logLevel: "info"

# -- Resource object
resources:
  {}
  # limits:
  #   cpu: 120m
  #   memory: 320Mi
  # requests:
  #   cpu: 80m
  #   memory: 120Mi
```

## Namespaced mode

By default, the operator will watch all namespaces, but it can be configured to watch only specific namespace or multiple namespaces.

If you want to override this behavior, specify the namespace:

- in the `WATCH_NAMESPACE` environment variable.
- in the `watchNamespace` field in the `values.yaml` file of helm-charts.

The operator supports comma separated namespace names for this setting.

If namespaced mode is enabled, operator uses a limited set of features:
- it cannot make any cluster wide API calls.
- it cannot assign rbac permissions for `vmagent`. It must be done manually via serviceAccount for vmagent.
- it ignores namespaceSelector fields at CRD objects and uses `WATCH_NAMESPACE` value for object matching.

At each namespace operator must have a set of required permissions, an example can be found at [this file](https://github.com/VictoriaMetrics/operator/blob/master/config/examples/operator_rbac_for_single_namespace.yaml).

## Monitoring of cluster components

By default, operator creates [VMServiceScrape](https://docs.victoriametrics.com/operator/resources/vmservicescrape/) 
object for each component that it manages.

You can disable this behaviour with `VM_DISABLESELFSERVICESCRAPECREATION` environment variable:

```shell
VM_DISABLESELFSERVICESCRAPECREATION=false
```

Also, you can override default configuration for self-scraping with `ServiceScrapeSpec` field in each deployable resource 
(`vmcluster/select`, `vmcluster/insert`, `vmcluster/storage`, `vmagent`, `vmalert`, `vmalertmanager`, `vmauth`, `vmsingle`):

## CRD Validation

Operator supports validation admission webhook [docs](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)

It checks resources configuration and returns errors to caller before resource will be created at kubernetes api.
This should reduce errors and simplify debugging.

Validation hooks at operator side must be enabled with flags:

```sh
./operator
    --webhook.enable
    # optional configuration for certDir and tls names.
    --webhook.certDir=/tmp/k8s-webhook-server/serving-certs/
    --webhook.keyName=tls.key
    --webhook.certName=tls.crt
```

You have to mount correct certificates at give directory.
It can be simplified with cert-manager and kustomize command:

```sh
kustomize build config/deployments/webhook/
```

## Requirements

- Valid certificate with key must be provided to operator
- Valid CABundle must be added to the `ValidatingWebhookConfiguration`


## Useful links

- [k8s admission webhooks](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)


---

The following legacy links are retained for historical reference.

###### List of command-line flags

Moved to [operator/configuration/#flags](https://docs.victoriametrics.com/operator/configuration/#flags)
