# VMAuth

The `VMAuth` CRD provides mechanism for exposing application with authorization to outside world or to other applications inside kubernetes cluster.

For first case, user can configure `ingress` setting at `VMAuth` CRD. For second one, operator will create secret with `username` and `password` at `VMUser` CRD name.
So it will be possible to access these credentials from any application by targeting corresponding kubernetes secret.

## Specification

You can see the full actual specification of the `VMAuth` resource in
the [API docs -> VMAuth](https://docs.victoriametrics.com/operator/api.html#vmauth).

## High availability

The `VMAuth` resource is stateless, so it can be scaled horizontally by increasing the number of replicas:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: vmauth-example
spec:
    replicas: 3
    # ...
```
