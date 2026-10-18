---
weight: 25
title: VMMCP
menu:
  docs:
    identifier: operator-cr-vmmcp
    parent: operator-cr
    weight: 25
aliases:
  - /operator/resources/vmmcp/
  - /operator/resources/vmmcp/index.html
tags:
  - kubernetes
  - metrics
---
`VMMCP` represents [MCP server for VictoriaMetrics](https://github.com/VictoriaMetrics/mcp-victoriametrics).
The MCP server implements [Model Context Protocol](https://modelcontextprotocol.io/), which lets AI assistants and agents
query VictoriaMetrics, explore metrics, labels and alerting rules, analyze cardinality
and search in VictoriaMetrics documentation.

For each `VMMCP` resource, the Operator deploys a `Deployment` with the MCP server in the same namespace
and adds `Service`, `ServiceAccount` and `VMServiceScrape` prefixed with `vmmcp-` and the name from `VMMCP.metadata.name`.

## Specification

You can see the full actual specification of the `VMMCP` resource in the **[API docs -> VMMCP](https://docs.victoriametrics.com/operator/api/#v1-vmmcp)**.

The MCP server is configured with environment variables, so `spec.extraArgs` isn't supported.
If you can't find necessary field in the specification of the custom resource,
set [MCP server environment variables](https://github.com/VictoriaMetrics/mcp-victoriametrics#configuration) at `spec.extraEnvs`.
They override environment variables with the same names set by the Operator.

Also, you can check out the [examples](https://docs.victoriametrics.com/operator/resources/vmmcp/#examples) section.

## Connecting to VictoriaMetrics

`spec.entrypoint` defines the root URL of VictoriaMetrics instance, which is queried by the MCP server,
and `spec.instanceType` defines its type: `single` (default) or `cluster`.

For [VMSingle](https://docs.victoriametrics.com/operator/resources/vmsingle/) set the URL of its service:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: example
spec:
  entrypoint: http://vmsingle-example.default.svc:8428
```

For [VMCluster](https://docs.victoriametrics.com/operator/resources/vmcluster/) set the URL of `vmselect` service and `cluster` instance type.
`spec.defaultTenantID` defines the tenant for requests, which don't specify it explicitly:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: example
spec:
  entrypoint: http://vmselect-example.default.svc:8481
  instanceType: cluster
  defaultTenantID: "0"
```

If VictoriaMetrics requires authentication, for example when it's accessed via [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth/),
set a `Secret` key with a token at `spec.bearerToken` or custom headers in `key=value` format at `spec.headers`:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: example
spec:
  entrypoint: http://vmauth-example.default.svc:8427
  bearerToken:
    name: vmmcp-token
    key: token
  headers:
    - X-Scope=team-a
```

`spec.passthroughHeaders` defines names of headers, which are forwarded from MCP client requests to VictoriaMetrics.
It allows serving several VictoriaMetrics instances by one MCP server
with [routing by header in VMAuth](https://docs.victoriametrics.com/victoriametrics/vmauth/#routing-by-header).

To work with [VictoriaMetrics Cloud](https://docs.victoriametrics.com/victoriametrics-cloud/) deployments,
set a `Secret` key with [API key](https://docs.victoriametrics.com/victoriametrics-cloud/api/) at `spec.cloudAPIKey` instead of `spec.entrypoint`:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: cloud
spec:
  cloudAPIKey:
    name: vmcloud
    key: api-key
```

## Connecting MCP clients

By default, the MCP server uses Streamable HTTP transport, which is served at `/mcp` path of `vmmcp-<name>` service on port `8080`.
Set `spec.serverMode: sse` for clients, which support only Server-Sent Events transport, it's served at `/sse` and `/message` paths.

For example, to connect [Claude Code](https://docs.anthropic.com/en/docs/claude-code/mcp) from your machine,
forward the service port and add the MCP server:

```sh
kubectl port-forward svc/vmmcp-example 8080:8080
claude mcp add --transport http victoriametrics http://localhost:8080/mcp
```

See [MCP server docs](https://github.com/VictoriaMetrics/mcp-victoriametrics#setup-in-clients) for other clients.

## Security

The MCP server doesn't authenticate its clients, so anyone with network access to it can query VictoriaMetrics
with credentials of the MCP server. Don't expose the MCP server outside of the cluster as is.
Put [VMAuth](https://docs.victoriametrics.com/operator/resources/vmauth/) with
[VMUser](https://docs.victoriametrics.com/operator/resources/vmuser/) in front of it
and restrict access to its pods with `spec.networkPolicy`:

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMUser
metadata:
  name: mcp
spec:
  username: mcp
  passwordRef:
    name: vmmcp-users
    key: password
  targetRefs:
    - static:
        url: http://vmmcp-example.default.svc:8080
      paths:
        - /mcp
```

## Tools and resources

The MCP server disables some tools by default, see the [list of tools](https://github.com/VictoriaMetrics/mcp-victoriametrics#toolset).
`spec.disabledTools` replaces the default list of disabled tools.
To enable all the tools, set `MCP_DISABLED_TOOLS` environment variable to an empty value at `spec.extraEnvs`.

The `documentation` tool searches in VictoriaMetrics documentation, which is built into the MCP server.
The MCP server loads the documentation index into memory at start: it takes about 700MiB of memory
and the start takes from 10 seconds with 1 CPU to more than a minute with 0.2 CPU.
Default resources and startup probe of `VMMCP` account for it.
Add `documentation` to `spec.disabledTools` to disable the tool, then the MCP server takes tens of MiB of memory and starts in a second.

`spec.disableResources: true` disables MCP resources with documentation, while the `documentation` tool keeps working.

## High availability

The MCP server is stateless, so `VMMCP` can run several replicas with `spec.replicaCount`
and spread them across nodes with `spec.topologySpreadConstraints`, `spec.affinity` and `spec.podDisruptionBudget`.

In `http` server mode, tool calls of a session can be served by any replica.
In `sse` server mode, MCP client messages must be served by the replica, which holds the client SSE connection,
so run a single replica or set `sessionAffinity: ClientIP` for the service with `spec.serviceSpec`.

## Version management

To set `VMMCP` version add `spec.image.tag` name from [releases](https://github.com/VictoriaMetrics/mcp-victoriametrics/releases):

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: example
spec:
  image:
    repository: ghcr.io/victoriametrics/mcp-victoriametrics
    tag: v1.20.2
    pullPolicy: Always
  # ...
```

The default version can be changed with `VM_MCP_METRICS_VERSION` operator parameter.
The MCP server image is published only to `ghcr.io`, so `VM_CONTAINERREGISTRY` operator parameter doesn't affect it.
Set `VM_VMMCPDEFAULT_IMAGE` operator parameter to use a mirror.

Also, you can specify `imagePullSecrets` if you are pulling images from private repo:

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: example
spec:
  imagePullSecrets:
    - name: my-repo-secret
  # ...
```

## Resource management

You can specify resources for each `VMMCP` resource in the `spec` section of the `VMMCP` CRD.

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: resources-example
spec:
  # ...
  resources:
    requests:
      memory: "768Mi"
      cpu: "100m"
    limits:
      memory: "1Gi"
      cpu: "500m"
  # ...
```

If these parameters are not specified, then,
by default all `VMMCP` pods have resource requests and limits from the default values of the following [operator parameters](https://docs.victoriametrics.com/operator/configuration/):

- `VM_VMMCPDEFAULT_RESOURCE_LIMIT_MEM` - default memory limit for `VMMCP` pods,
- `VM_VMMCPDEFAULT_RESOURCE_LIMIT_CPU` - default CPU limit for `VMMCP` pods,
- `VM_VMMCPDEFAULT_RESOURCE_REQUEST_MEM` - default memory request for `VMMCP` pods,
- `VM_VMMCPDEFAULT_RESOURCE_REQUEST_CPU` - default CPU request for `VMMCP` pods.

These default parameters will be used if:

- `VM_VMMCPDEFAULT_USEDEFAULTRESOURCES` is set to `true` (default value),
- `VMMCP` CR doesn't have `resources` field in `spec` section.

Field `resources` in `VMMCP` spec have higher priority than operator parameters.

If you set `VM_VMMCPDEFAULT_USEDEFAULTRESOURCES` to `false` and don't specify `resources` in `VMMCP` CRD,
then `VMMCP` pods will be created without resource requests and limits.

Also, you can specify requests without limits - in this case default values for limits will not be used.

## Examples

### VMMCP for VMCluster

```yaml
apiVersion: operator.victoriametrics.com/v1
kind: VMMCP
metadata:
  name: example
spec:
  entrypoint: http://vmselect-example.default.svc:8481
  instanceType: cluster
  replicaCount: 2
  podDisruptionBudget:
    maxUnavailable: 1
  disabledTools:
    - export
    - flags
    - test_rules
  logFormat: json
  # allows connections only from vmauth and scrapes of MCP server metrics by vmagent
  networkPolicy:
    ingress:
      - from:
          - podSelector:
              matchLabels:
                app.kubernetes.io/name: vmauth
          - podSelector:
              matchLabels:
                app.kubernetes.io/name: vmagent
```
