## VMAgent relabel


`VMAgent` supports global relabeling for all metrics and per remoteWrite target relabel config. 

 It also has some extra options, you can check it [docs](https://github.com/VictoriaMetrics/VictoriaMetrics/blob/master/app/vmagent/README.md#relabeling)

 First, You have to create `Confimap` with relabeling configuration
 
 ```yaml
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: vmagent-relabel
data:
  global-relabel.yaml: |
        - target_label: bar
        - source_labels: [aa]
          separator: "foobar"
          regex: "foo.+bar"
          target_label: aaa
          replacement: "xxx"
        - action: keep
          source_labels: [aaa]
        - action: drop
          source_labels: [aaa]
  target-1-relabel.yaml: |
        - action: keep_if_equal
          source_labels: [foo, bar]
        - action: drop_if_equal
          source_labels: [foo, bar]
EOF
```

Second, add `relabelConfig` to `VMagent` spec for global relabeling with name of `Configmap` - `vmagent-relabel` and key `global-relabel.yaml`.
 For relabeling per remoteWrite target, add   `urlRelabelConfig` name of `Configmap` - `vmagent-relabel` and key `target-1-relabel.yaml` to one of remoteWrite target for relabeling only
for those target.

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
  relabelConfig:
    name: "vmagent-relabel"
    key: "global-relabel.yaml"
  remoteWrite:
    - url: "http://vmsingle-example-vmsingle-persisted.default.svc:8429/api/v1/write"
    - url: "http://vmsingle-example-vmsingle.default.svc:8429/api/v1/write"
      urlRelabelConfig:
        name: "vmagent-relabel"
        key: "target-1-relabel.yaml"
EOF
```