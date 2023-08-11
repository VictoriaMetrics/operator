---
sort: 3
weight: 3
title: Security
---

# Security

## Access control

### Roles

To run in a cluster the operator needs certain permissions, you can see them in [this directory](https://github.com/VictoriaMetrics/operator/tree/master/config/rbac):

- [`role.yaml` file](https://github.com/VictoriaMetrics/operator/blob/master/config/rbac/role.yaml) - basic set of cluster roles for launching an operator.
- [`leader_election_role.yaml` file](https://github.com/VictoriaMetrics/operator/blob/master/config/rbac/leader_election_role.yaml) - set of roles with permissions to do leader election (is necessary to run the operator in several replicas for high availability).

Also in [the same directory](https://github.com/VictoriaMetrics/operator/tree/master/config/rbac) are files with a set of separate permissions to view or edit [operator resources](https://docs.victoriametrics.com/vmoperator/resources/) to organize fine-grained access:

- file `<RESOURCE_NAME>_viewer_role.yaml` - permissions for viewing (`get`, `list` and `watch`) some resource of vmoperator.
- file `<RESOURCE_NAME>_editor_role.yaml` - permissions for editing (`create`, `delete`, `patch`, `update` and `deletecollection`) some resource of vmoperator (also includes viewing permissions).

For instance, [`vmalert_editor_role.yaml` file](https://github.com/VictoriaMetrics/operator/blob/master/config/rbac/vmalert_editor_role.yaml) contain permission
for editing [`vmagent` custom resources](https://docs.victoriametrics.com/vmoperator/resources/vmagent.html).

**TODO**

<!-- TODO: namespace-wide -->
<!-- TODO: service accounts / role bindings? -->
<!-- TODO: resource/roles relations -->
<!-- TODO: strict pod security -->
<!-- TODO: the section below needs to be updated -->

## Security policies

VictoriaMetrics operator provides several security features, such as [PodSecurityPolicies](https://kubernetes.io/docs/concepts/policy/pod-security-policy/),
[PodSecurityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).

### PodSecurityPolicy.

By default, operator creates serviceAccount for each cluster resource and binds default `PodSecurityPolicy` to it.

Default psp:
```yaml
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: vmagent-example-vmagent
spec:
  allowPrivilegeEscalation: false
  fsGroup:
    rule: RunAsAny
  hostNetwork: true
  requiredDropCapabilities:
  - ALL
  runAsUser:
    rule: RunAsAny
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    rule: RunAsAny
  volumes:
  - persistentVolumeClaim
  - secret
  - emptyDir
  - configMap
  - projected
  - downwardAPI
  - nfs
```

This behaviour may be disabled with env variable passed to operator:
 ```yaml
 - name: VM_PSPAUTOCREATEENABLED
   value: "false"
```

User may also override default pod security policy with setting: `spec.podSecurityPolicyName: "psp-name"`.

## PodSecurityContext

`PodSecurityContext` can be configured with spec setting. It may be useful for mounted volumes, with `VMSingle` for example:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: vmsingle-f
  namespace: monitoring-system
spec:
  retentionPeriod: "2"
  removePvcAfterDelete: true
  securityContext:
      runAsUser: 1000
      fsGroup: 1000
      runAsGroup: 1000
  extraArgs:
    dedup.minScrapeInterval: 10s
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 25Gi
  resources:
    requests:
      cpu: "0.5"
      memory: "512Mi"
    limits:
      cpu: "1"
      memory: "1512Mi"

```
