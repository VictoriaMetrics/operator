

## Basic auth for targets

To authenticate a `ServiceMonitor`s over a metrics endpoint use [`basicAuth`](../api.md#basicauth)

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    k8s-apps: basic-auth-example
  name: basic-auth-example
spec:
  endpoints:
  - basicAuth:
      password:
        name: basic-auth
        key: password
      username:
        name: basic-auth
        key: user
    port: metrics
  selector:
    matchLabels:
      app: myapp
EOF
```

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: basic-auth
data:
  password: dG9vcg== # toor
  user: YWRtaW4= # admin
type: Opaque
EOF
```
