# VMAuth

The `VMAuth` CRD provides mechanism for exposing application with authorization to outside world or to other applications inside kubernetes cluster.
For first case, user can configure `ingress` setting at `VMAuth` CRD. For second one, operator will create secret with `username` and `password` at `VMUser` CRD name.
So it will be possible to access these credentials from any application by targeting corresponding kubernetes secret.

## Specification

You can see the full actual specification of the `VMAuth` resource in
the [API docs -> VMAuth](https://docs.victoriametrics.com/vmoperator/api.html#vmauth).

**TODO**
