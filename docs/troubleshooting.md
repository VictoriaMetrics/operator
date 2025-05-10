---
weight: 15
title: Troubleshooting
menu:
  docs:
    parent: "operator"
    weight: 13
---

This document provides troubleshooting guides for the most common issues encountered when working with the VictoriaMetrics Operator.
For more general troubleshooting, refer to the [VictoriaMetrics Troubleshooting](https://docs.victoriametrics.com/victoriametrics/troubleshooting/).

## Unexpected vmstorage cache invalidation or inconsistent cache max size

Sometimes, vmstorage may [show unexpected cache resets or different cache sizes across pods](https://github.com/VictoriaMetrics/VictoriaMetrics/issues/8883). 
This often happens after a pod restarts and you may notice changes in the `vm_cache_size_max_bytes` and `vm_cache_size_bytes metrics`.
Normally, vmstorage saves its cache when it shuts down properly and loads it again on startup. 
But if the amount of available memory at startup is different from before, this behavior changes. 
In those cases, vmstorage throws away the saved cache and creates a new one. 
This causes higher memory usage until the cache is rebuilt.

A common reason this happens is when vmstorage pods have different memory requests and limits. For example:

```yaml
resources:
  limits:
    cpu: "8"
    memory: 96Gi
  requests:
    cpu: "8"
    memory: 64Gi
```
To avoid these issues, make sure all vmstorage pods have the same memory limits and requests.

