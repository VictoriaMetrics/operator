# VMNodeScrape

The `VMNodeScrape` CRD provides discovery mechanism for scraping metrics kubernetes nodes.

`VMNodeScrape` object generates part of [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html) configuration.
It has various options for scraping configuration of target (with basic auth,tls access, by specific port name etc.).

By specifying configuration at CRD, operator generates config 
for [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html) and syncs it. It's useful for cadvisor scraping,
node-exporter or other node-based exporters. `VMAgent` `nodeScrapeSelector` must match `VMNodeScrape` labels.

More information about selectors you can find in [this doc](https://docs.victoriametrics.com/operator/resources/vmagent.html#scraping).

## Specification

You can see the full actual specification of the `VMNodeScrape` resource in
the [API docs -> VMNodeScrape](https://docs.victoriametrics.com/operator/api.html#vmnodescrape).

<!-- TODO: examples -->
