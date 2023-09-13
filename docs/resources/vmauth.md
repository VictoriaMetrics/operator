# VMAuth

The `VMAuth` CRD provides mechanism for exposing application with authorization to outside world or to other applications inside kubernetes cluster.

For first case, user can configure `ingress` setting at `VMAuth` CRD. For second one, operator will create secret with `username` and `password` at `VMUser` CRD name.
So it will be possible to access these credentials from any application by targeting corresponding kubernetes secret.

## Specification

You can see the full actual specification of the `VMAuth` resource in
the [API docs -> VMAuth](https://docs.victoriametrics.com/operator/api.html#vmauth).

## Users

The CRD specifies which `VMUser`s should be covered by the deployed `VMAuth` instances based on label selection.
The Operator then generates a configuration based on the included `VMUser`s and updates the `Configmaps` containing
the configuration. It continuously does so for all changes that are made to `VMUser`s or to the `VMAuth` resource itself.

[VMUser](https://docs.victoriametrics.com/operator/resources/vmrule.html) objects are generates part of [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth.html) configuration.

For filtering users `VMAuth` uses selectors `userNamespaceSelector` and `userSelector`.
It allows configuring rules access control across namespaces and different environments.
Specification of selectors you can see in [this doc](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#labelselector-v1-meta).

In addition to the above selectors, the filtering of objects in a cluster is affected by the field `selectAllByDefault` of `VMAuth` spec and environment variable `WATCH_NAMESPACE` for operator.

Following rules are applied:

| `userNamespaceSelector` | `userSelector` | `selectAllByDefault` | `WATCH_NAMESPACE` | Selected rules                                                                                     |
|-------------------------|----------------|----------------------|-------------------|----------------------------------------------------------------------------------------------------|
| undefined               | undefined      | false                | undefined         | nothing                                                                                            |
| undefined               | undefined      | **true**             | undefined         | all users in the cluster                                                                           |
| **defined**             | undefined      | any                  | undefined         | all users are matching at namespaces for given `userNamespaceSelector`                             |
| undefined               | **defined**    | any                  | undefined         | all users only at `VMAuth`'s namespace are matching for given `userSelector`                       |
| **defined**             | **defined**    | any                  | undefined         | all users only at namespaces matched `userNamespaceSelector` for given `userSelector` are matching |
| any                     | undefined      | any                  | **defined**       | all users only at `VMAuth`'s namespace                                                             |
| any                     | **defined**    | any                  | **defined**       | all users only at `VMAuth`'s namespace for given `userSelector` are matching                       |

More details about `WATCH_NAMESPACE` variable you can read in [this doc](https://docs.victoriametrics.com/operator/configuration.html#namespaced-mode).

Here are some examples of `VMAuth` configuration with selectors:

```yaml
# select all scrape objects in the cluster
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: vmauth-select-all
spec:
  # ...
  selectAllByDefault: true

---

# select all scrape objects in specific namespace (my-namespace)
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAuth
metadata:
  name: vmauth-select-ns
spec:
  # ...
  userNamespaceSelector: 
    matchLabels:
      kubernetes.io/metadata.name: my-namespace
```

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
