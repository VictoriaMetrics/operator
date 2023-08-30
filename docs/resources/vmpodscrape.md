# VMPodScrape

The `VMPodScrape` CRD allows to declaratively define how a dynamic set of pods should be monitored.
Use label selections to match pods for scraping. This allows an organization to introduce conventions
for how metrics should be exposed. Following these conventions new services will be discovered automatically without
need to reconfigure.

A `Pod` is a collection of one or more containers which can expose Prometheus metrics on a number of ports.

The `VMPodScrape` object discovers pods and generates the relevant scraping configuration.

The `PodMetricsEndpoints` section of the `VMPodScrapeSpec` is used to configure which ports of a pod are going to be
scraped for metrics and with which parameters.

Both `VMPodScrapes` and discovered targets may belong to any namespace. It is important for cross-namespace monitoring
use cases, e.g. for meta-monitoring. Using the `namespaceSelector` of the `VMPodScrapeSpec` one can restrict the
namespaces from which `Pods` are discovered from. To discover targets in all namespaces the `namespaceSelector` has to
be empty:

```yaml
spec:
  namespaceSelector:
    any: true
```

## Specification

You can see the full actual specification of the `VMPodScrape` resource in
the [API docs -> VMPodScrape](https://docs.victoriametrics.com/operator/api.html#vmpodscrape).

**TODO**

